# tsgo

A TeamSpeak 3 bot framework, it is a WIP. In order to be able to run command from all accounts the bot must use a
serveradmin user

## Basic Example
```go
package main

import (
	"fmt"
	"log"
	"strings"

	"code.jakeyoungdev.com/jake/tsgo"
)

func main() {
	bot, err := tsgo.New(tsgo.Config{
		Address:  "IP address",
		Port:     "10011", //Server query port
		Username: "serveradmin",
		Password: "password",
		Server:   1,
	})
	if err != nil {
		log.Fatal(err)
	}

	bot.AddHandler(func(m tsgo.Message) (string, error) {
		fmt.Printf("%s: %s\n", m.InvokerName, m.Msg)
		if strings.HasPrefix(m.Msg, ".") {
			switch m.Msg {
			case ".ping":
				return "pong", nil
			case ".test":
				return "tost", nil
			}
		}
		return "", nil
	})

	if err := bot.Start(); err != nil {
		log.Fatal(err)
	}
}

```