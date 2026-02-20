/*
tsgo is a TeamSpeak 3 bot framework that uses the TS3 Server Query protocol to interact with the server, it
enables bots to listen to messages in text channels
*/
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
	//use to pass credentials to the TeamSpeak server
	LOGIN_STRING = "login %s %s"
	//server command to enable message events in text channels, this allows
	//the message alerts to come to the bot
	MSG_LISTENER_STRING = "servernotifyregister event=textchannel"
	//determines which server the bot should use
	SERVER_USE_STRING = "use %d"
	//the 'action' string for messages being sent in channel texts
	MSG_ACTION = "notifytextmessage"
	//the read deadline to be used on the underlying tcp connection, setting this
	//prevents the read() call from hanging indefinitely and allows more graceful
	//exits/disconnects
	MSG_READ_DEADLINE = time.Minute * 1
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

// Msg parses the text from a message alert, cleaning the data before returning. Message
// strings utilize '\s' instead of whitespaces, these are replaced with proper whitespaces
// before the message data is returned
func (r *response) Msg() string {
	msg, ok := r.Data["msg"]
	if !ok {
		return ""
	}

	return strings.ReplaceAll(msg, "\\s", " ")
}

type Config struct {
	//the server address and port to be used as the connection string to
	//in the format <address>:<port>
	Address string
	Port    string
	//TeamSpeak credentials to authenticate the bot
	Username string
	Password string
	//the server identifier to be used in the 'use' command, this tells the
	//bot which server to use. The port option is currently not supported by
	//tsgo and the server ID must be used
	Server int
}

// Message contains the needed fields for notifytextmessage events

// Message is the parsed fields from a notifytextmessage event, this
// struct is passed to the message handler functions
type Message struct {
	Msg         string
	InvokerID   int
	InvokerName string
	InvokerUID  string
}

// Handler is the custom type provided to easily handle notifytextmessage events,
// the raw server string is parsed into a Message struct and forwarded to the
// handler functions. If a string is returned, and no error is encountered, then
// the returned string will be written back to the server as a response
type Handler func(m Message) (string, error)

type tsBot struct {
	server net.Conn
	reader *bufio.Scanner
	//used to trigger bot shutdown
	kill chan os.Signal
	//essentially used as a waitgroup to ensure the listener routine
	//finishes before the bot fully shuts down
	clean   chan struct{}
	cfg     Config
	handler Handler
}

type TsBot interface {
	read() (string, error)
	write(msg string) error
	listen() error
	Start() error
	login() error
	close() error
	parseResponse(res string) (*response, error)
	AddHandler(f Handler)
}

// New creates a new bot instance, verifying that the required configuration fields are present before
// continuing. The bot requires address, port, username, and password to properly function
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

// read attempts to read data from the server connection, if data is present, it is returned
func (t *tsBot) read() (string, error) {
	if t.server == nil || t.reader == nil {
		return "", ErrNotConnected
	}

	if t.reader.Scan() {
		return t.reader.Text(), nil
	}

	return "", nil
}

// write handles writing messages back to the remote server. All TeamSpeak messages must end
// in a newline character, it is automatically added if it is missing
func (t *tsBot) write(msg string) error {
	if t.server == nil || t.reader == nil {
		return ErrNotConnected
	}

	//ensure the newline suffix is present before sending
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}

	//messages must end in newlines, this should probably eventually check for it before adding it
	_, err := t.server.Write([]byte(msg))
	return err
}

// listen configures the server to receive message events. To do this two messages are sent to the server: first is to configure
// which TeamSpeak server the bot should attach itself to, the second configures the bot to listen for notifytextmessage events.
// If configuration is successful, a listener routine is spawned to continuously parse and handle messages until the bot is shutdown
func (t *tsBot) listen() error {
	err := t.writeSuccess(fmt.Sprintf(SERVER_USE_STRING, t.cfg.Server))
	if err != nil {
		return err
	}

	err = t.writeSuccess(MSG_LISTENER_STRING)
	if err != nil {
		return err
	}

	//a listener routine that loops until the kill channel is hit, parsing all messages and responding as
	//needed. A read deadline is set after each read to prevent locking, the clean channel will be hit once
	//the routine has finished completely
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

			//prevent the bot from responding to itself
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

// Start creates the tcp connection to the teamspeak server and initializes the scanner to read from the server. On successful
// connection it will read two lines to consume the welcome banner to prevent errors parsing irregular messages. A login attempt
// is the made and, if successful, listen for messages. This is a blocking call that will listen for messages until the kill channel
// is hit, it will then handle safe shutdown disconnecting from the server
func (t *tsBot) Start() error {
	conn, err := net.Dial(PROTOCOL, net.JoinHostPort(t.cfg.Address, t.cfg.Port))
	if err != nil {
		return err
	}

	t.server = conn
	t.reader = bufio.NewScanner(conn)

	//read teamspeak 'welcome' banner
	for x := 0; x < BANNER_LENGTH; x++ {
		if _, err := t.read(); err != nil {
			return err
		}
	}

	err = t.login()
	if err != nil {
		return err
	}

	err = t.listen()
	if err != nil {
		return err
	}

	<-t.kill
	//resend out the interrupt signal as an added measure since the above line will consume the
	//initial one. This is an added failsafe to encourage the go routine to clean itself up
	t.kill <- os.Interrupt
	return t.close()
}

// login send the authentication message to the server, erroring if login was not successful
func (t *tsBot) login() error {
	return t.writeSuccess(fmt.Sprintf(LOGIN_STRING, t.cfg.Username, t.cfg.Password))
}

// close handles proper bot shutdown, the underlying tcp connection is closed to timeout any read calls
// waiting in the listener routine. This allows the routine to be cleaned up without waiting for the read
// deadline to be triggered, close will wait for the routine to signal its cleaned up before returning
func (t *tsBot) close() error {
	err := t.server.Close()
	<-t.clean
	return err
}

// writeSuccess writes data to this server and checks for a successful response. The server reply
// is then parsed and checked for an "ok" field of an error action, returning an error if an error
// is found
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
		return fmt.Errorf("%e, msg: %s", err, res)
	}

	//replies are of the "error" action type even if they are successful
	if parsed.Action != "error" {
		return fmt.Errorf("%e: %s", ErrInvalidResponse, res)
	}

	if parsed.Msg() != "ok" {
		return fmt.Errorf("server returned an error code: %s", parsed.Msg())
	}

	return nil
}

// parseResponse takes the string received from the server and unmarshal'd into
// the response struct for easier consumption. Messages from the server are seperated
// by whitespaces and contain key=value fields
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

// AddHandler configures the message handler function to forward server
// messages to
func (t *tsBot) AddHandler(f Handler) {
	t.handler = f
}
