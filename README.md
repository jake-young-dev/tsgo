# tsgo

A TeamSpeak 3 bot framework, it is a WIP. In order to be able to run command from all accounts the bot must use a
serveradmin user

## Basic Example
```go
package main

import (
	"log"
	"strings"

	"code.jakeyoungdev.com/jake/tsgo"
)

func main() {
	bot, err := tsgo.New(tsgo.Config{
		Address:  "192.168.0.1",
		Port:     "10011",
		Username: "serveradmin",
		Password: "password",
		Server: 1,
	})
	if err != nil {
		log.Fatal(err)
	}

	bot.AddHandler(func(m tsgo.Message) (string, error) {
		if strings.HasPrefix(m.Msg, ".") {
			switch m.Msg {
			case ".ping":
				return "pong", nil
			}
		}
		return "", nil
	})

	if err := bot.Start(); err != nil {
		log.Fatal(err)
	}

	if err := bot.Close(); err != nil {
		log.Fatal(err)
	}
}

```