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
