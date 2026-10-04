package main

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-smtp"
)

func TestFailedName(t *testing.T) {
	when := time.Date(2026, 10, 4, 15, 4, 5, 120000000, time.UTC)
	name := failedName(when)
	if !strings.HasPrefix(name, "20261004T150405.12Z_") || !strings.HasSuffix(name, ".txt") {
		t.Fatalf("name = %s", name)
	}
}

func TestRenderMessageKeepsAttachmentNamesOnly(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"From: sender@example.com",
		"To: a@example.com",
		"Subject: hi",
		"MIME-Version: 1.0",
		"Content-Type: multipart/mixed; boundary=abc",
		"",
		"--abc",
		"Content-Type: text/plain; charset=utf-8",
		"",
		"please see the report",
		"--abc",
		"Content-Type: application/pdf; name=\"report.pdf\"",
		"Content-Disposition: attachment; filename=\"report.pdf\"",
		"Content-Transfer-Encoding: base64",
		"",
		"JVBERi0xLjQK",
		"--abc--",
		"",
	}, "\r\n"))

	got := renderMessage(raw)
	if !strings.Contains(got, "please see the report") {
		t.Fatalf("missing body:\n%s", got)
	}
	if !strings.Contains(got, "Attachments:\n- report.pdf\n") {
		t.Fatalf("missing filename:\n%s", got)
	}
	if strings.Contains(got, "JVBERi0xLjQK") {
		t.Fatalf("attachment bytes leaked:\n%s", got)
	}
}

func TestReturnPathOverrideFailedFile(t *testing.T) {
	raw := []byte(strings.Join([]string{
		"Return-Path:",
		" <bounce@diamondway.com.au>",
		"From: \"Administrator\" <notifications@diamondway.com.au>",
		"To: syahrial@portcities.net",
		"Subject: Test",
		"",
		"hello",
		"",
	}, "\r\n"))
	sets, err := parseHeaderSet("Return-Path: bounce@daisysgarden.com.au\nReply-To: admin@daisysgarden.com.au\n")
	if err != nil {
		t.Fatal(err)
	}
	rewritten := applyHeaderSet(raw, sets)
	from := envelopeFrom("bounce@diamondway.com.au", sets)
	if from != "bounce@daisysgarden.com.au" {
		t.Fatalf("envelope = %s", from)
	}
	if got := envelopeFrom("old@example.com", []headerSet{{Name: "Return-Path", Value: "<bounce@daisysgarden.com.au>"}}); got != "bounce@daisysgarden.com.au" {
		t.Fatalf("angle address = %s", got)
	}
	got := string(renderFailed(failedMail{
		When:   time.Date(2026, 10, 4, 15, 49, 21, 0, time.UTC),
		Client: "10.10.207.74:36502",
		Helo:   "[10.10.207.74]",
		From:   from,
		Rcpts:  []string{"syahrial@portcities.net"},
		Err: &smtp.SMTPError{
			Code:         550,
			EnhancedCode: smtp.EnhancedCode{5, 7, 1},
			Message:      "Invalid login",
		},
		Raw: rewritten,
	}))
	if strings.Contains(got, "bounce@diamondway.com.au") {
		t.Fatalf("original address kept:\n%s", got)
	}
	if !strings.Contains(got, "mail-from = bounce@daisysgarden.com.au\n") {
		t.Fatalf("missing envelope note:\n%s", got)
	}
	if !strings.Contains(got, "Return-Path: bounce@daisysgarden.com.au\n") {
		t.Fatalf("missing overridden header:\n%s", got)
	}
	if !strings.Contains(got, "Reply-To: admin@daisysgarden.com.au\n") {
		t.Fatalf("missing inserted header:\n%s", got)
	}
	if strings.Contains(got, "Mail-From:") {
		t.Fatalf("preamble still looks like a header:\n%s", got)
	}
}

func TestParseHeaderSetRejectsBadLine(t *testing.T) {
	if _, err := parseHeaderSet("Return Path: bounce@daisysgarden.com.au"); err == nil {
		t.Fatal("expected a bad line to fail")
	}
}

func TestSMTPFailureAppendsFilename(t *testing.T) {
	err := smtpFailure(&smtp.SMTPError{
		Code:         550,
		EnhancedCode: smtp.EnhancedCode{5, 1, 1},
		Message:      "user unknown",
	}, "20261004T150405.00Z_ab12cd34.txt")
	if err.Code != 550 || err.EnhancedCode != (smtp.EnhancedCode{5, 1, 1}) {
		t.Fatalf("code = %d %v", err.Code, err.EnhancedCode)
	}
	if err.Message != "user unknown (saved as 20261004T150405.00Z_ab12cd34.txt)" {
		t.Fatalf("message = %q", err.Message)
	}

	plain := smtpFailure(errors.New("dial tcp: connection refused"), "file.txt")
	if plain.Code != 451 || !strings.Contains(plain.Message, "(saved as file.txt)") {
		t.Fatalf("plain = %d %q", plain.Code, plain.Message)
	}
}
