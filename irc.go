package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"

	"gopkg.in/sorcix/irc.v2"
)

type IRC struct {
	conn      *irc.Conn
	errors    chan error
	log       *log.Logger
	connected chan struct{} // closed when the connection handshake is finished
	nick      string
	channel   string // TODO: multiple channels
}

// TODO: what to do upon disconnection?
// TODO: set timeouts on connection?

func NewIRC(addr string, nick string) (*IRC, error) {
	conn, err := irc.DialTLS(addr, nil)
	if err != nil {
		return nil, err
	}

	c := &IRC{
		conn:      conn,
		errors:    make(chan error, 1),
		log:       log.New(ioutil.Discard, "", 0),
		connected: make(chan struct{}),
		nick:      nick,
		channel:   "#bot", // reasonable default
	}

	return c, nil
}

func (c *IRC) SetLogger(logger *log.Logger) {
	c.log = logger
}

// Set the channel to connect to on login.
// Must be called before Run.
func (c *IRC) SetChannel(channel string) {
	if channel == "" {
		return
	}
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}
	c.channel = channel
}

func (c *IRC) handle(m *irc.Message) {
	c.log.Println("<<", m.String())
	if m.Command == "001" {
		// 001 is a welcome event, so we join channels there
		c.send(&irc.Message{
			Command: "JOIN",
			Params:  []string{c.channel},
		})
		close(c.connected)
	} else if m.Command == "PRIVMSG" && m.Trailing() == "!quit" {
		io.WriteString(c.conn, "QUIT")
	} else if m.Command == "PRIVMSG" {
		c.send(&irc.Message{
			Command: "PRIVMSG",
			Params:  []string{c.channel, m.Trailing()},
		})
	}
}

func (c *IRC) send(m *irc.Message) error {
	c.log.Println(">>", m.String())
	return c.conn.Encode(m)
}

func (c *IRC) Run() error {
	fmt.Fprintf(c.conn, "NICK :%s", c.nick)
	fmt.Fprintf(c.conn, "USER %s - - :%s", c.nick, c.nick)
	err := c.runLoop()
	return err
}

func (c *IRC) runLoop() error {
	for {
		m, err := c.conn.Decode()
		if err != nil {
			return err
		}

		if m.Command == "PING" {
			c.log.Println("ping")
			m.Command = "PONG"
			c.conn.Encode(m)
		} else {
			c.handle(m)
		}
	}
}

// Send a message to all channels.
// If message contains newlines, it will be split into multiple messages.
// Safe to call concurrently.
func (c *IRC) Announce(msg string) error {
	<-c.connected
	if strings.Contains(msg, "\n") {
		for _, line := range strings.Split(msg, "\n") {
			if line != "" {
				err := c.send(&irc.Message{
					Command: "PRIVMSG",
					Params:  []string{c.channel, line},
				})
				if err != nil {
					return err
				}
			}
		}
		return nil
	} else {
		return c.send(&irc.Message{
			Command: "PRIVMSG",
			Params:  []string{c.channel, msg},
		})
	}
}
