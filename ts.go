package tsgo

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strings"
	"time"
)

const (
	PROTOCOL      = "tcp"
	BANNER_LENGTH = 2
	//sets the event notification for messages in text channels
	MSG_LISTENER_STRING = "servernotifyregister event=textchannel"
	//determines which "server" to use, need to figure out what this means
	SERVER_USE_STRING = "use 1"
)

var (
	ErrInvalidResponse = errors.New("the server returned an unexpected response")
	ErrNotConnected    = errors.New("the Connect method must be called before communicating with the server")
	ErrUnauthorized    = errors.New("there was an error attempting to login to the server")
)

type Response struct {
	Action string
	Data   map[string]string
}

type Config struct {
	Address  string
	Username string
	Password string
}

type tsBot struct {
	server net.Conn
	reader *bufio.Scanner
	kill   chan os.Signal
}

type TsBot interface {
	Connect(cfg Config) error
	Close() error
	Start() error
	login(user, password string) error
	read() (string, error)
	write(msg string) error
	writeAndRead(msg string) (Response, error)
	parseResponse(res string) (Response, error)
	parseMsgField(msg string) error
}

func NewTsBot() TsBot {
	k := make(chan os.Signal, 1)
	signal.Notify(k, os.Interrupt)
	return &tsBot{
		kill: k,
	}
}

func (t *tsBot) Connect(cfg Config) error {
	conn, err := net.Dial(PROTOCOL, cfg.Address)
	if err != nil {
		return err
	}
	t.server = conn

	rdr := bufio.NewScanner(conn)
	t.reader = rdr

	//read teamspeak banner
	for x := 0; x < 2; x++ {
		if _, err := t.read(); err != nil {
			return err
		}
	}

	err = t.login(cfg.Username, cfg.Password)
	if err != nil {
		return err
	}

	//these are config commands for the bot setup, we can ignore the responses
	_, err = t.writeAndRead(SERVER_USE_STRING)
	if err != nil {
		return err
	}

	_, err = t.writeAndRead(MSG_LISTENER_STRING)
	return err
}

func (t *tsBot) Close() error {
	return t.server.Close()
}

func (t *tsBot) Start() error {
	go func() {
		for len(t.kill) == 0 {
			t.server.SetReadDeadline(time.Now().Add(time.Second * 10))
			res, err := t.read()
			if err != nil && !os.IsTimeout(err) {
				fmt.Println(err)
				break
			}

			if res == "" || res == "\n" {
				continue
			}

			err = t.parseMsgField(res)
			if err != nil {
				fmt.Println(err)
				continue
			}
		}
	}()

	<-t.kill

	//resend to cleanup goroutine
	t.kill <- os.Interrupt
	time.Sleep(time.Second * 5)

	return nil
}

func (t *tsBot) login(user, password string) error {
	loginStr := fmt.Sprintf("login %s %s", user, password)

	res, err := t.writeAndRead(loginStr)
	if err != nil {
		return err
	}

	if res.Data["msg"] != "ok" {
		return ErrUnauthorized
	}

	return nil
}

func (t *tsBot) read() (string, error) {
	if t.server == nil || t.reader == nil {
		return "", ErrNotConnected
	}

	if t.reader.Scan() {
		return t.reader.Text(), nil
	}

	return "", nil
}

func (t *tsBot) write(msg string) error {
	if t.server == nil {
		return ErrNotConnected
	}

	_, err := t.server.Write([]byte(msg + "\n"))
	return err
}

func (t *tsBot) writeAndRead(msg string) (Response, error) {
	err := t.write(msg)
	if err != nil {
		return Response{}, err
	}

	res, err := t.read()
	if err != nil {
		return Response{}, err
	}

	return t.parseResponse(res)
}

func (t *tsBot) parseResponse(res string) (Response, error) {
	fields := strings.Fields(res)
	if len(fields) == 0 {
		return Response{}, nil
	}

	r := Response{
		Action: fields[0],
	}
	fields = fields[1:]

	vals := make(map[string]string)
	for _, f := range fields {
		spl := strings.Split(f, "=")
		if len(spl) < 2 {
			return Response{}, ErrInvalidResponse
		}
		vals[spl[0]] = strings.TrimSpace(spl[1])
	}

	r.Data = vals

	return r, nil
}

func (t *tsBot) parseMsgField(msg string) error {
	r, err := t.parseResponse(msg)
	if err != nil {
		return err
	}

	msg, ok := r.Data["msg"]
	if !ok {
		return nil
	}

	msg = strings.ReplaceAll(msg, "\\s", " ")
	fmt.Println(msg)

	return nil
}
