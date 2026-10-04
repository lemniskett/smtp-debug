package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-smtp"
)

type config struct {
	Listen      string
	Upstream    string
	Host        string
	User        string
	Pass        string
	SSL         bool
	StartTLS    bool
	InsecureTLS bool
	FailedDir   string
	Domain      string
	MaxBytes    int64
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(cfg.FailedDir, 0o755); err != nil {
		log.Fatal(err)
	}

	log.Printf("listening on %s, relaying to %s", cfg.Listen, cfg.Upstream)

	server := smtp.NewServer(&backend{cfg: cfg})
	server.Addr = cfg.Listen
	server.Domain = cfg.Domain
	server.MaxMessageBytes = cfg.MaxBytes
	server.ReadTimeout = 10 * time.Minute
	server.WriteTimeout = 10 * time.Minute
	server.EnableSMTPUTF8 = true

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func loadConfig() (config, error) {
	upstream := os.Getenv("SMTP_DEBUG_UPSTREAM")
	if upstream == "" {
		return config{}, errors.New("SMTP_DEBUG_UPSTREAM is required")
	}
	host, _, err := net.SplitHostPort(upstream)
	if err != nil {
		return config{}, errors.New("SMTP_DEBUG_UPSTREAM must be host:port")
	}

	ssl, err := envBool("SMTP_DEBUG_SSL")
	if err != nil {
		return config{}, err
	}
	startTLS, err := envBool("SMTP_DEBUG_STARTTLS")
	if err != nil {
		return config{}, err
	}
	if ssl && startTLS {
		return config{}, errors.New("SMTP_DEBUG_SSL and SMTP_DEBUG_STARTTLS cannot both be set")
	}
	insecure, err := envBool("SMTP_DEBUG_INSECURE_TLS")
	if err != nil {
		return config{}, err
	}

	maxBytes, err := envInt64("SMTP_DEBUG_MAX_BYTES", 26214400)
	if err != nil {
		return config{}, err
	}
	if maxBytes <= 0 {
		return config{}, errors.New("SMTP_DEBUG_MAX_BYTES must be greater than 0")
	}

	return config{
		Listen:      env("SMTP_DEBUG_LISTEN", "127.0.0.1:2525"),
		Upstream:    upstream,
		Host:        host,
		User:        os.Getenv("SMTP_DEBUG_UPSTREAM_USER"),
		Pass:        os.Getenv("SMTP_DEBUG_UPSTREAM_PASS"),
		SSL:         ssl,
		StartTLS:    startTLS,
		InsecureTLS: insecure,
		FailedDir:   env("SMTP_DEBUG_FAILED_DIR", "/failed"),
		Domain:      env("SMTP_DEBUG_DOMAIN", "localhost"),
		MaxBytes:    maxBytes,
	}, nil
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBool(key string) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true, nil
	case "0", "false", "no":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true, false, 1, 0, yes, or no", key)
	}
}

func envInt64(key string, fallback int64) (int64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return n, nil
}

type backend struct {
	cfg config
}

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &session{
		cfg:    b.cfg,
		client: c.Conn().RemoteAddr().String(),
		helo:   c.Hostname(),
		conn:   c.Conn(),
	}, nil
}

type session struct {
	cfg    config
	client string
	helo   string
	conn   net.Conn
	from   string
	rcpts  []string
}

func (s *session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	s.rcpts = nil
	return nil
}

func (s *session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.rcpts = append(s.rcpts, to)
	return nil
}

func (s *session) Data(r io.Reader) error {
	raw, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	if s.conn != nil {
		_ = s.conn.SetDeadline(time.Time{})
	}

	err = relay(s.cfg, s.from, s.rcpts, raw)
	if err == nil {
		return nil
	}

	name, saveErr := saveFailed(s.cfg.FailedDir, failedMail{
		When:   time.Now(),
		Client: s.client,
		Helo:   s.helo,
		From:   s.from,
		Rcpts:  s.rcpts,
		Err:    err,
		Raw:    raw,
	})
	if saveErr != nil {
		log.Printf("failed to save mail: %v", saveErr)
		return smtpFailure(err, "")
	}
	log.Printf("upstream rejected mail, saved %s", name)
	return smtpFailure(err, name)
}

func (s *session) Reset() {
	s.from = ""
	s.rcpts = nil
}

func (s *session) Logout() error {
	return nil
}

func smtpFailure(err error, name string) *smtp.SMTPError {
	var smtpErr *smtp.SMTPError
	out := &smtp.SMTPError{
		Code:         451,
		EnhancedCode: smtp.EnhancedCode{4, 4, 1},
		Message:      oneLine(err.Error()),
	}
	if errors.As(err, &smtpErr) {
		out.Code = smtpErr.Code
		out.EnhancedCode = smtpErr.EnhancedCode
		out.Message = oneLine(smtpErr.Message)
	}
	if name == "" {
		return out
	}
	suffix := "(saved as " + name + ")"
	if out.Message == "" {
		out.Message = "saved as " + name
	} else {
		out.Message = out.Message + " " + suffix
	}
	const max = 450
	if len(out.Message) > max {
		keep := len(suffix) + 1
		if keep >= max {
			out.Message = suffix
			return out
		}
		out.Message = strings.TrimSpace(out.Message[:max-keep]) + " " + suffix
	}
	return out
}
