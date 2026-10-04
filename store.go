package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
)

type failedMail struct {
	When   time.Time
	Client string
	Helo   string
	From   string
	Rcpts  []string
	Err    error
	Raw    []byte
}

func saveFailed(dir string, msg failedMail) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	body := renderFailed(msg)
	for range 5 {
		name := failedName(msg.When)
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		_, werr := f.Write(body)
		cerr := f.Close()
		if werr != nil {
			return "", werr
		}
		if cerr != nil {
			return "", cerr
		}
		return name, nil
	}
	return "", fmt.Errorf("could not allocate a filename in %s", dir)
}

func failedName(t time.Time) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return t.UTC().Format("20060102T150405.00Z") + "_" + hex.EncodeToString(buf[:]) + ".txt"
}

func renderFailed(msg failedMail) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "received-at = %s\n", msg.When.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "client = %s\n", msg.Client)
	fmt.Fprintf(&b, "helo = %s\n", msg.Helo)
	fmt.Fprintf(&b, "mail-from = %s\n", msg.From)
	for _, rcpt := range msg.Rcpts {
		fmt.Fprintf(&b, "rcpt-to = %s\n", rcpt)
	}
	fmt.Fprintf(&b, "upstream-error = %s\n", upstreamError(msg.Err))
	b.WriteString("---\n")
	b.WriteString(renderMessage(msg.Raw))
	return []byte(b.String())
}

func upstreamError(err error) string {
	if err == nil {
		return ""
	}
	var smtpErr *smtp.SMTPError
	if !errors.As(err, &smtpErr) {
		return oneLine(err.Error())
	}
	code := smtpErr.EnhancedCode
	if code == smtp.EnhancedCodeNotSet || code == smtp.NoEnhancedCode {
		return oneLine(fmt.Sprintf("%d %s", smtpErr.Code, smtpErr.Message))
	}
	return oneLine(fmt.Sprintf("%d %d.%d.%d %s", smtpErr.Code, code[0], code[1], code[2], smtpErr.Message))
}

func renderMessage(raw []byte) string {
	header, ok := headerBlock(raw)
	if !ok {
		return "Body could not be parsed.\n"
	}
	parsed, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return header + "\nBody could not be parsed.\n"
	}

	mediaType, params, parseErr := mime.ParseMediaType(parsed.Header.Get("Content-Type"))
	if parseErr != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		if isText(mediaType, parseErr) {
			body, err := readText(parsed.Body, parsed.Header.Get("Content-Transfer-Encoding"))
			if err != nil {
				return header + "\nBody could not be parsed.\n"
			}
			return joinMessage(header, []string{body}, nil)
		}
		return joinMessage(header, nil, []string{headerAttachmentName(parsed.Header, mediaType)})
	}

	texts, names, err := walkParts(parsed.Body, params["boundary"])
	if err != nil {
		return header + "\nBody could not be parsed.\n"
	}
	return joinMessage(header, texts, names)
}

func headerBlock(raw []byte) (string, bool) {
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return strings.ReplaceAll(string(raw[:i]), "\r\n", "\n"), true
	}
	if i := bytes.Index(raw, []byte("\n\n")); i >= 0 {
		return string(raw[:i]), true
	}
	return "", false
}

func walkParts(r io.Reader, boundary string) (texts, names []string, err error) {
	if boundary == "" {
		return nil, nil, fmt.Errorf("missing boundary")
	}
	mr := multipart.NewReader(r, boundary)
	for {
		part, perr := mr.NextPart()
		if perr == io.EOF {
			return texts, names, nil
		}
		if perr != nil {
			return nil, nil, perr
		}
		mediaType, params, parseErr := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if parseErr == nil && strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
			childText, childNames, werr := walkParts(part, params["boundary"])
			if werr != nil {
				return nil, nil, werr
			}
			texts = append(texts, childText...)
			names = append(names, childNames...)
			continue
		}
		if isText(mediaType, parseErr) {
			body, rerr := readText(part, part.Header.Get("Content-Transfer-Encoding"))
			if rerr != nil {
				return nil, nil, rerr
			}
			texts = append(texts, body)
			continue
		}
		_, _ = io.Copy(io.Discard, part)
		names = append(names, leafName(part.FileName(), mediaType))
	}
}

func isText(mediaType string, parseErr error) bool {
	if parseErr != nil || mediaType == "" {
		return true
	}
	switch strings.ToLower(mediaType) {
	case "text/plain", "text/html":
		return true
	default:
		return false
	}
}

func readText(r io.Reader, encoding string) (string, error) {
	decoded, err := io.ReadAll(decodeTransfer(r, encoding))
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(string(decoded), "\r\n", "\n"), nil
}

func decodeTransfer(r io.Reader, encoding string) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		return base64.NewDecoder(base64.StdEncoding, newWhitespaceStripper(r))
	case "quoted-printable":
		return quotedprintable.NewReader(r)
	default:
		return r
	}
}

type whitespaceStripper struct {
	r   io.Reader
	buf []byte
	err error
}

func newWhitespaceStripper(r io.Reader) io.Reader {
	return &whitespaceStripper{r: r}
}

func (s *whitespaceStripper) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.buf) == 0 && s.err == nil {
		var tmp [512]byte
		n, err := s.r.Read(tmp[:])
		s.err = err
		for _, c := range tmp[:n] {
			if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
				continue
			}
			s.buf = append(s.buf, c)
		}
	}
	if len(s.buf) == 0 {
		err := s.err
		if err == nil {
			err = io.EOF
		}
		return 0, err
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func leafName(filename, mediaType string) string {
	if filename != "" {
		return oneLine(filename)
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return "(unnamed) " + strings.ToLower(mediaType)
}

func headerAttachmentName(h mail.Header, mediaType string) string {
	if _, params, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil {
		if name := decodeWords(params["filename"]); name != "" {
			return name
		}
	}
	if _, params, err := mime.ParseMediaType(h.Get("Content-Type")); err == nil {
		if name := decodeWords(params["name"]); name != "" {
			return name
		}
	}
	return leafName("", mediaType)
}

func decodeWords(v string) string {
	if v == "" {
		return ""
	}
	decoded, err := new(mime.WordDecoder).DecodeHeader(v)
	if err != nil {
		return oneLine(v)
	}
	return oneLine(decoded)
}

func joinMessage(header string, texts, names []string) string {
	var b strings.Builder
	b.WriteString(header)
	if !strings.HasSuffix(header, "\n") {
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	for i, text := range texts {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(text)
		if text != "" && !strings.HasSuffix(text, "\n") {
			b.WriteByte('\n')
		}
	}
	if len(names) > 0 {
		if len(texts) > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("Attachments:\n")
		for _, name := range names {
			b.WriteString("- ")
			b.WriteString(name)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
