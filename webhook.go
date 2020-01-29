package main

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

type Webhook struct {
	Root    string
	Logger  *log.Logger
	Handler WebhookHandler
	Secret  []byte
}

type WebhookHandler func(event string, body []byte)

func (h *Webhook) Serve(l net.Listener) error {
	srv := &http.Server{
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      h,
		ErrorLog:     h.Logger,
	}
	return srv.Serve(l)
}

const apache = "2/Jan/2006:15:04:05 -0700"

func (h *Webhook) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()

	h.logf("%v - - [%v] %q", req.RemoteAddr, time.Now().Format(apache), fmt.Sprintln(req.Method, req.RequestURI, req.Proto))
	h.logf("Headers: %v", req.Header)
	event := req.Header.Get("X-GitHub-Event")
	if event == "" {
		h.logf("received request with no X-GitHub-Event header")
		http.Error(w, "error: missing event header", http.StatusBadRequest)
		return
	}
	h.logf("Event: %q", event)

	// check the payload signature
	sig := req.Header.Get("X-Hub-Signature")
	if sig == "" {
		h.logf("received %q event with no X-Hub-Signature header", event)
		http.Error(w, "error: no signature", http.StatusForbidden)
		return
	}
	if !strings.HasPrefix(sig, "sha1=") {
		h.logf("malformed signature or unsupported hash function")
		http.Error(w, "error: malformed signature", http.StatusForbidden)
		return
	}
	sigBytes, err := hex.DecodeString(strings.TrimPrefix(sig, "sha1="))
	if err != nil {
		h.logf("malformed signature: %v", err)
		http.Error(w, "error: malformed signature", http.StatusForbidden)
		return
	}
	if len(sigBytes) != sha1.Size {
		h.logf("malformed signature: too short/long")
		http.Error(w, "error: malformed signature", http.StatusForbidden)
		return
	}

	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		h.logf("error reading body")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if h.Secret == nil {
		panic("webhook: Secret must not be nil")
	}
	mac := hmac.New(sha1.New, h.Secret)
	mac.Write(body)
	if !hmac.Equal(sigBytes, mac.Sum(nil)) {
		h.logf("received %q event with invalid signature", event)
		http.Error(w, "error: bad signature request", http.StatusForbidden)
		return
	}

	if h.Handler != nil {
		h.Handler(event, body)
	}
}

func (h *Webhook) logf(format string, v ...interface{}) {
	if h.Logger != nil {
		h.Logger.Printf(format, v...)
	} else {
		log.Printf(format, v...)
	}
}
