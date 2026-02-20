# tsgo

tsgo is a TeamSpeak 3 bot framework that utilizes the Server Query protocol to support text commands, tsgo is in development and may
encounter breaking changes until v1 release

## Basic Example
```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/jake-young-dev/tsgo"
)

func main() {
	bot, err := tsgo.New(tsgo.Config{
		Address:  "127.0.0.1",
		Port:     "10011",
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
				return "testing", nil
			}
		}
		return "", nil
	})

	if err := bot.Start(); err != nil {
		log.Fatal(err)
	}
}

```