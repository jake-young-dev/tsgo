# tsgo

tsgo is a bot framework for TeamSpeak 3 servers utilizing the Server Query protocol. Enabling easy creation of text-based bots that support
custom commands. As of right now, this framework is very basic and intended for basic bots, it is in development and may encounter breaking
changes until a v1 release.

## Documentation
### Config
A basic configuration struct is provided for bots
```go
type Config struct {
	//the server address and port of the TeamSpeak server
	Address string
	Port    string
	//TeamSpeak credentials to authenticate the bot in the server
	Username string
	Password string
	//the server identifier to be used in the 'use' command, this tells the
	//bot which server to listen to. TeamSpeak supports a port option as the identifier but it is not supported yet, this must be the Server ID
	Server int
}
```

### Handlers
Currently the only handler supported by tsgo is the message handler, a message is defined as:
```go
// Message is the parsed message from a notifytextmessage event, this
// struct is passed to the message handler functions
type Message struct {
	Msg         string
	InvokerID   int
	InvokerName string
	InvokerUID  string
}
```
```go
// MsgHandler is the custom type provided to easily handle notifytextmessage events
type MsgHandler func(m Message) (string, error)
```

### Login
The Login method dials the TeamSpeak server and attempts to login using the provided credentials, if successful, it will set the server using the 'use' command
```go
func (b *bot) Login() error
```

### ConfigureMessageEvents
ConfigureMessageEvents configures the bot to receive message alerts in text channels and sets the message handler function if not nil. If no message handler is provided then the message data is logged to standard output
```go
func (b *bot) ConfigureMessageEvents(h MsgHandler) error
```

### Start/Stop
The Start method spawns the listener routine to accept data messages from the TeamSpeak server, this method is blocking and will wait until an os.Interrupt signal is received. The Start method handles proper cleanup and closes the connection to the server
```go
func (b *bot) Start() error
```

## Basic Example
A basic working example of a bot that parses two commands responding to both
```go
package main

import (
	"fmt"
	"log"
	"strings"

	tsgo "github.com/jake-young-dev/tsgo"
)

func main() {
	bot, err := tsgo.New(tsgo.Config{
		Address:  "ip/url",
		Port:     "port",
		Username: "serveradmin",
		Password: "password",
		Server:   1,
	})
	if err != nil {
		log.Fatal(err)
	}

	err = bot.Login()
	if err != nil {
		log.Fatal(err)
	}

	err = bot.ConfigureMessageEvents(func(m tsgo.Message) (string, error) {
		fmt.Printf("%s: %s\n", m.InvokerName, m.Msg)
		if strings.HasPrefix(m.Msg, ".") {
			switch m.Msg {
			case ".ping":
				return "pong", nil
			case ".test":
				return "testing", nil
			}
		}
		return "", nil
	})
	if err != nil {
		log.Fatal(err)
	}

	err = bot.Start()
	if err != nil {
		log.Fatal(err)
	}
}
```