package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const secretFile = "webhook.secret"
const secretSize = 256 / 8

var secretSizeBase64 = base64.StdEncoding.EncodedLen(secretSize)

var botLog = log.New(os.Stderr, "[bot] ", log.LstdFlags)

func main() {
	ircUrl := flag.String("irc", "", "irc server url (including nick and channel)")
	debugFlag := flag.Bool("debug", false, "print debug logs")
	flag.Parse()

	if *ircUrl == "" {
		log.Fatalf("no -irc option provided")
		return
	}

	// Read server secret
	secret, err := ioutil.ReadFile(secretFile)
	if err != nil {
		secretBytes := make([]byte, secretSize)
		rand.Read(secretBytes)
		// base64 encode so that we can copy/paste into github config
		secret = []byte(base64.StdEncoding.EncodeToString(secretBytes))
		if err := ioutil.WriteFile(secretFile, secret, 0400); err != nil {
			log.Fatalln("error writing server secret:", err)
		}
		absPath, err := filepath.Abs(secretFile)
		if err != nil {
			absPath = secretFile
		}
		log.Printf("generated a new secret in %s", absPath)
	}
	if len(secret) != secretSizeBase64 {
		log.Fatalf("error: server secret is not the expected size; want %d found %d", secretSizeBase64, len(secret))
	}

	run(secret, *ircUrl, *debugFlag)
}

func run(secret []byte, ircUrl string, debug bool) {
	l, err := listen()
	if err != nil {
		log.Fatal(err)
	}

	irc, err := newIRCFromURL(ircUrl)
	if err != nil {
		log.Fatal(err)
	}
	if debug {
		irc.SetLogger(log.New(os.Stderr, "[irc] ", log.LstdFlags))
	}

	type githubEvent struct {
		Type string
		Body []byte
	}

	events := make(chan githubEvent, 10)

	h := &Webhook{
		Root:   "/webhook",
		Logger: log.New(os.Stderr, "[webhook] ", log.LstdFlags),
		Handler: func(event string, body []byte) {
			select {
			case events <- githubEvent{Type: event, Body: body}:
			default:
			}
		},
		Secret: secret,
	}

	// main loop
	go func() {
		for event := range events {
			reportEvent(irc, event.Type, event.Body)
		}
	}()

	go func() {
		log.Fatal(h.Serve(l))
	}()

	if err := irc.Run(); err != nil {
		if err != io.EOF {
			log.Fatal(err)
		}
	}
}

func reportEvent(irc *IRC, eventType string, body []byte) {
	gh, err := ParseGithubEvent(body)
	if err != nil {
		botLog.Printf("error parsing %s event: %v", eventType, err)
		botLog.Printf("payload body: %q", body)
		return
	}
	msg := FormatGithubEvent(eventType, gh, nil)
	if msg == "" {
		botLog.Printf("ignoring %s event", eventType)
		return
	}
	err = irc.Announce(msg)
	if err != nil {
		botLog.Printf("error sending message for %s event: %v", eventType, err)
		return
	}
}

func newIRCFromURL(ircUrl string) (*IRC, error) {
	u, err := url.Parse(ircUrl)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "irc" && u.Scheme != "ircs" {
		return nil, fmt.Errorf("not an irc url: %q", ircUrl)
	}
	addr := u.Host
	if u.Port() == "" {
		if u.Scheme == "irc" {
			addr = net.JoinHostPort(addr, "6667")
		} else if u.Scheme == "ircs" {
			addr = net.JoinHostPort(addr, "6697")
		}
	}
	nick := u.User.Username()
	if nick == "" {
		return nil, fmt.Errorf("irc url is missing a username: %q", ircUrl)
	}
	channel := strings.TrimLeft(u.Path, "/")
	// note: IRC.SetChannel takes care of prepending a "#"

	c, err := NewIRC(addr, nick)
	if err != nil {
		return nil, err
	}
	c.SetChannel(channel)
	return c, nil
}
