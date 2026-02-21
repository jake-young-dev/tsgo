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
)

const (
	PROTOCOL = "tcp"
	//length of TeamSpeak welcome banner
	BANNER_LENGTH = 2
	//TeamSpeak server login
	LOGIN_STRING = "login %s %s"
	//TeamSpeak command to enable message events from text channels
	MSG_LISTENER_STRING = "servernotifyregister event=textchannel"
	//TeamSpeak command to select server
	SERVER_USE_STRING = "use %d"
	//the 'action' string for messages being sent in channel texts, this
	//is the type forwarded to the message handlers
	MSG_ACTION = "notifytextmessage"
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

// Msg parses the text from a notifytextmessage event, cleaning the '\s' delimeters from the message
// data before returning
func (r *response) Msg() string {
	msg, ok := r.Data["msg"]
	if !ok {
		return ""
	}

	return strings.ReplaceAll(msg, "\\s", " ")
}

type Config struct {
	//the server address and port of the TeamSpeak server
	Address string
	Port    string
	//TeamSpeak credentials to authenticate the bot in the server
	Username string
	Password string
	//the server identifier to be used in the 'use' command, this tells the
	//bot which server to listen to. TeamSpeak supports a port option as the identifier
	//but it is not supported yet, this must be the Server ID
	Server int
}

// Message is the parsed message from a notifytextmessage event, this
// struct is passed to the message handler functions
type Message struct {
	Msg         string
	InvokerID   int
	InvokerName string
	InvokerUID  string
}

// MsgHandler is the custom type provided to easily handle notifytextmessage events
type MsgHandler func(m Message) (string, error)

type bot struct {
	server     net.Conn
	reader     *bufio.Scanner
	kill       chan os.Signal
	clean      chan struct{}
	cfg        Config
	msgHandler MsgHandler
}

type Bot interface {
	Login() error
	ConfigureMessageEvents(h MsgHandler) error
	Start() error

	read() (string, error)
	write(msg string) error
	close() error
	parseResponse(res string) (*response, error)
	login() error
	listen() error
}

// New creates a new bot instance, verifying that the required configuration fields are present before
// continuing. The bot requires address, port, username, and password to properly function
func New(cfg Config) (Bot, error) {
	if cfg.Address == "" || cfg.Password == "" || cfg.Username == "" || cfg.Port == "" {
		return nil, errors.New("invalid configuration, ensure all fields are present")
	}
	k := make(chan os.Signal, 1)
	return &bot{
		kill:  k,
		cfg:   cfg,
		clean: make(chan struct{}, 1),
		msgHandler: func(m Message) (string, error) {
			fmt.Printf("%s: %s\n", m.InvokerName, m.Msg)
			return "", nil
		},
	}, nil
}

// Login dials the TeamSpeak server and initializes the connection reader. TeamSpeak sends a welcome banner
// on successful connection, it is automatically before sending the authentication and server selection commands
func (b *bot) Login() error {
	conn, err := net.Dial(PROTOCOL, net.JoinHostPort(b.cfg.Address, b.cfg.Port))
	if err != nil {
		return err
	}

	b.server = conn
	b.reader = bufio.NewScanner(conn)

	//read teamspeak 'welcome' banner
	for x := 0; x < BANNER_LENGTH; x++ {
		if _, err := b.read(); err != nil {
			return err
		}
	}

	err = b.login()
	if err != nil {
		return err
	}

	return b.writeSuccess(fmt.Sprintf(SERVER_USE_STRING, b.cfg.Server))
}

// ConfigureMessageEvents applies a custom message handler, if provided, and configures the TeamSpeak server
// to send message events to the bot
func (b *bot) ConfigureMessageEvents(h MsgHandler) error {
	if h != nil {
		b.msgHandler = h
	}

	err := b.writeSuccess(MSG_LISTENER_STRING)
	if err != nil {
		return err
	}

	return nil
}

// Start spawns listener routine then waits for system interrupt. Once interrupt is received
// the listener routine is killed and cleaned up closing the connection to the server
func (b *bot) Start() error {
	err := b.listen()
	if err != nil {
		return err
	}

	//wait for initial interrupt
	wait := make(chan os.Signal, 1)
	signal.Notify(wait, os.Interrupt)
	<-wait

	//forward interrupt to allow the listener routine loop to exit
	b.kill <- os.Interrupt
	err = b.close()
	if err != nil {
		return err
	}

	return nil
}

// read data from the server
func (b *bot) read() (string, error) {
	if b.server == nil || b.reader == nil {
		return "", ErrNotConnected
	}

	if b.reader.Scan() {
		return b.reader.Text(), nil
	}

	return "", nil
}

// write handles writing messages back to the remote server. All TeamSpeak messages must end
// in a newline character, it is automatically added if it is missing
func (b *bot) write(msg string) error {
	if b.server == nil || b.reader == nil {
		return ErrNotConnected
	}

	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}

	_, err := b.server.Write([]byte(msg))
	return err
}

// close handles proper bot shutdown, the underlying tcp connection is closed to timeout any read calls
// waiting in the listener routine
func (b *bot) close() error {
	err := b.server.Close()
	if err != nil {
		return err
	}
	<-b.clean
	return nil
}

// writeSuccess writes data to the server then checks for a success response
func (b *bot) writeSuccess(msg string) error {
	err := b.write(msg)
	if err != nil {
		return err
	}

	res, err := b.read()
	if err != nil {
		return err
	}

	parsed, err := b.parseResponse(res)
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

// parseResponse takes the string received from the server and unmarshal's it into
// the response struct for easier consumption. Messages from the server are seperated
// by whitespaces and contain key=value fields after the action token
func (b *bot) parseResponse(res string) (*response, error) {
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

// login sends the authentication message to the server
func (b *bot) login() error {
	return b.writeSuccess(fmt.Sprintf(LOGIN_STRING, b.cfg.Username, b.cfg.Password))
}

// listen spawns a go routine to continuously read data from the server
func (b *bot) listen() error {
	go func() {
		for len(b.kill) == 0 {
			res, err := b.read()
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

			r, err := b.parseResponse(res)
			if err != nil {
				fmt.Println(err)
				continue
			}

			//prevent the bot from responding to itself, this can get weird when using names outside
			//of serveradmin or specific account names. It may need to be hardcoded
			if r.Data["invokeruid"] == b.cfg.Username {
				continue
			}

			if r.Action == MSG_ACTION {
				invId, err := strconv.Atoi(r.Data["invokerid"])
				if err != nil {
					fmt.Println(err)
					continue
				}

				reply, err := b.msgHandler(Message{
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
					//move the command string to a const
					err := b.write(fmt.Sprintf("sendtextmessage targetmode=2 target=%d msg=%s", invId, strings.ReplaceAll(reply, " ", "\\s")))
					if err != nil {
						fmt.Println(err)
						continue
					}
				}
			}
		}
		b.clean <- struct{}{}
	}()

	return nil
}
