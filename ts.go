package tsgo

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"
)

const (
	PROTOCOL = "tcp"
	//on successful connection the server sends a two-line welcome banner
	//we need to consume this to allow other messages to come through
	BANNER_LENGTH = 2
	//server command to enable message events in channels
	MSG_LISTENER_STRING = "servernotifyregister event=textchannel"
	//determines which server to watch
	SERVER_USE_STRING = "use 1"
	//the 'action' string for messages being sent in channel texts
	MSG_ACTION = "notifytextmessage"
	//the connection read deadline, it is used to prevent the listener routine from locking
	//forever, for some reason any low values prevent the connection from properly reading responses
	MSG_READ_DEADLINE = time.Second * 5
)

var (
	ErrInvalidResponse = errors.New("the server returned an unexpected response")
	ErrNotConnected    = errors.New("no server has been connected to, unable to read or write")
)

type response struct {
	//the action string is the descriptive key for messages e.g. error, notifytextmessage, etc.
	Action string
	//the data map contains all other key values fields from the server
	Data map[string]string
}

// Msg parses, the msg field from the message and cleans the space delimeters
func (r *response) Msg() string {
	msg, ok := r.Data["msg"]
	if !ok {
		return ""
	}

	return strings.ReplaceAll(msg, "\\s", " ")
}

// add port field and handle combination
type Config struct {
	Address  string
	Port     string
	Username string
	Password string
}

// Message contains the needed fields for notifytextmessage events
type Message struct {
	Msg         string
	InvokerID   int
	InvokerName string
	InvokerUID  string
}

// the Handler type represents the custom message handlers
type Handler func(m Message) (string, error)

type tsBot struct {
	server  net.Conn
	reader  *bufio.Scanner
	kill    chan os.Signal
	clean   chan struct{}
	cfg     Config
	handler Handler
}

type TsBot interface {
	read() (string, error)
	write(msg string) error
	listen() error
	Start() error
	Close() error
	parseResponse(res string) (*response, error)
	AddHandler(f Handler)
}

func New(cfg Config) (TsBot, error) {
	if cfg.Address == "" || cfg.Password == "" || cfg.Username == "" || cfg.Port == "" {
		return nil, errors.New("invalid configuration, ensure all fields are present")
	}
	k := make(chan os.Signal, 1)
	signal.Notify(k, os.Interrupt)
	return &tsBot{
		kill:  k,
		cfg:   cfg,
		clean: make(chan struct{}),
	}, nil
}

// read will check if there is data to be read from the connection and return the text
// to the client, it will error if the connection or reader are not set up
func (t *tsBot) read() (string, error) {
	if t.server == nil || t.reader == nil {
		return "", ErrNotConnected
	}

	if t.reader.Scan() {
		return t.reader.Text(), nil
	}

	return "", nil
}

// write sends data back to the server, the required newlines are automatically added
func (t *tsBot) write(msg string) error {
	if t.server == nil || t.reader == nil {
		return ErrNotConnected
	}

	//messages must end in newlines, this should probably eventually check for it before adding it
	_, err := t.server.Write([]byte(msg + "\n"))
	return err
}

// listen configures the server to listen to for message events and creates a go routine to listen for messages
// and pass them to the handler function the return value is sent as a reply to the command. The ReadDeadline is reset
// on each read or timeout to prevent the routine from locking for too long
func (t *tsBot) listen() error {
	//this needs to be moved to the config struct (the server number)
	//i need to figure out how to pull it better though
	err := t.writeSuccess(SERVER_USE_STRING)
	if err != nil {
		return err
	}

	err = t.writeSuccess(MSG_LISTENER_STRING)
	if err != nil {
		return err
	}

	go func() {
		for len(t.kill) == 0 {
			t.server.SetReadDeadline(time.Now().Add(MSG_READ_DEADLINE))
			res, err := t.read()
			if os.IsTimeout(err) {
				continue
			}
			if err != nil {
				fmt.Println(err)
				break
			}

			if res == "" || res == "\n" {
				continue
			}

			r, err := t.parseResponse(res)
			if err != nil {
				fmt.Println(err)
				continue
			}

			if r.Data["invokeruid"] == t.cfg.Username {
				continue
			}

			if r.Action == MSG_ACTION {
				invId, err := strconv.Atoi(r.Data["invokerid"])
				if err != nil {
					fmt.Println(err)
					continue
				}

				reply, err := t.handler(Message{
					Msg:         r.Msg(),
					InvokerID:   invId,
					InvokerName: r.Data["invokername"],
					InvokerUID:  r.Data["invokeruid"],
				})
				if err != nil {
					fmt.Println(err)
					continue
				}

				if reply != "" {
					err := t.write(fmt.Sprintf("sendtextmessage targetmode=2 target=%d msg=%s", invId, strings.ReplaceAll(reply, " ", "\\s")))
					if err != nil {
						fmt.Println(err)
						continue
					}
				}
			}
		}
		t.clean <- struct{}{}
	}()

	return nil
}

// Start dials the teamspeak server and initializes the connections reader. On connection it will consume the teamspeak
// welcome banner and attempt a login with the provided credentials and, if successful will listen for messages. This function
// is BLOCKING and all setup should be handled before
func (t *tsBot) Start() error {
	conn, err := net.Dial(PROTOCOL, net.JoinHostPort(t.cfg.Address, t.cfg.Port))
	if err != nil {
		return err
	}

	t.server = conn
	t.reader = bufio.NewScanner(conn)

	//read teamspeak banner
	for x := 0; x < 2; x++ {
		if _, err := t.read(); err != nil {
			return err
		}
	}

	err = t.writeSuccess(fmt.Sprintf("login %s %s", t.cfg.Username, t.cfg.Password))
	if err != nil {
		return err
	}

	err = t.listen()
	if err != nil {
		return err
	}

	<-t.kill
	//since we have already consumed the interrupt, send another to allow the listener
	//routine to finish
	t.kill <- os.Interrupt
	return nil
}

// Close waits for the listener go routine to finish and closes the connection to the server
func (t *tsBot) Close() error {
	<-t.clean
	return t.server.Close()
}

// writeSuccess writes the msg to the server and checks the response for an "ok" response
func (t *tsBot) writeSuccess(msg string) error {
	err := t.write(msg)
	if err != nil {
		return err
	}

	res, err := t.read()
	if err != nil {
		return err
	}

	parsed, err := t.parseResponse(res)
	if err != nil {
		return err
	}

	if parsed.Action != "error" {
		return fmt.Errorf("the server returned an unexpected response: %v", parsed)
	}

	if parsed.Msg() != "ok" {
		return fmt.Errorf("server returned an error code: %s", msg)
	}

	return nil
}

// parseResponse breaks the message strings into a response struct for handling
func (t *tsBot) parseResponse(res string) (*response, error) {
	fields := strings.Fields(res)
	if len(fields) == 0 {
		return nil, nil
	}

	r := &response{
		Action: fields[0],
	}
	fields = fields[1:]

	vals := make(map[string]string)
	for _, f := range fields {
		spl := strings.Split(f, "=")
		if len(spl) < 2 {
			return nil, ErrInvalidResponse
		}
		vals[spl[0]] = strings.TrimSpace(spl[1])
	}

	r.Data = vals

	return r, nil
}

// AddHandler adds message handler function to the bot
func (t *tsBot) AddHandler(f Handler) {
	t.handler = f
}
