package main

import (
	"bytes"
	"crypto/tls"
	"unicode"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"
)

func relay(cfg config, from string, rcpts []string, raw []byte) error {
	tlsConfig := &tls.Config{
		ServerName:         cfg.Host,
		InsecureSkipVerify: cfg.InsecureTLS,
	}

	var (
		client *smtp.Client
		err    error
	)
	switch {
	case cfg.SSL:
		client, err = smtp.DialTLS(cfg.Upstream, tlsConfig)
	case cfg.StartTLS:
		client, err = smtp.DialStartTLS(cfg.Upstream, tlsConfig)
	default:
		client, err = smtp.Dial(cfg.Upstream)
	}
	if err != nil {
		return err
	}
	defer client.Close()

	if err = client.Hello(cfg.Domain); err != nil {
		return err
	}
	if cfg.User != "" {
		auth := sasl.NewPlainClient("", cfg.User, cfg.Pass)
		if err = client.Auth(auth); err != nil {
			return err
		}
	}

	var opts *smtp.MailOptions
	if needsUTF8(from, rcpts) {
		opts = &smtp.MailOptions{UTF8: true}
	}
	if err = client.Mail(from, opts); err != nil {
		return err
	}
	for _, rcpt := range rcpts {
		if err = client.Rcpt(rcpt, nil); err != nil {
			return err
		}
	}

	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))); err != nil {
		_ = w.Close()
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}

func needsUTF8(from string, rcpts []string) bool {
	if !isASCII(from) {
		return true
	}
	for _, rcpt := range rcpts {
		if !isASCII(rcpt) {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}
