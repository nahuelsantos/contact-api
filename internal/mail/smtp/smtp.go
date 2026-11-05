// Package smtp sends mail over SMTP (plain, STARTTLS or implicit TLS).
package smtp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	stdmail "net/mail"
	netsmtp "net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/mail"
)

const defaultTimeout = 10 * time.Second

// Sentinel errors. None of them carry field values or message content.
var (
	// ErrAddress is returned when From or a recipient is not a valid address.
	ErrAddress = errors.New("smtp: invalid address")
	// ErrNoRecipients is returned when the message has no recipients.
	ErrNoRecipients = errors.New("smtp: no recipients")
	// ErrTLSMode is returned for an unknown tls setting.
	ErrTLSMode = errors.New("smtp: unknown tls mode")
)

// Provider implements mail.Provider over SMTP.
type Provider struct {
	name, host, tlsMode string
	port                int
	username, password  string
	timeout             time.Duration
	tlsConfig           *tls.Config
}

// Option customises a Provider. Options are for tests only.
type Option func(*Provider)

func withTLSConfig(c *tls.Config) Option { return func(p *Provider) { p.tlsConfig = c } }

func withTimeout(d time.Duration) Option { return func(p *Provider) { p.timeout = d } }

// New returns an SMTP provider. An empty TLS setting means "none".
func New(name string, c config.ProviderConfig, opts ...Option) *Provider {
	mode := c.TLS
	if mode == "" {
		mode = "none"
	}
	p := &Provider{
		name: name, host: c.Host, port: c.Port, tlsMode: mode,
		username: c.Username, password: c.Password,
		timeout: defaultTimeout,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.name }

// Send delivers m. The whole exchange is bounded by the shorter of the
// provider timeout (10 s by default) and the context deadline. Errors never
// contain message content.
func (p *Provider) Send(ctx context.Context, m mail.Message) error {
	switch p.tlsMode {
	case "none", "starttls", "implicit":
	default:
		return ErrTLSMode
	}
	from, err := stdmail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("%w: from", ErrAddress)
	}
	if m.ReplyTo != "" {
		if _, err := stdmail.ParseAddress(m.ReplyTo); err != nil {
			return fmt.Errorf("%w: reply-to", ErrAddress)
		}
	}
	if len(m.To) == 0 {
		return ErrNoRecipients
	}
	rcpts := make([]string, 0, len(m.To))
	for _, t := range m.To {
		a, err := stdmail.ParseAddress(t)
		if err != nil {
			return fmt.Errorf("%w: to", ErrAddress)
		}
		rcpts = append(rcpts, a.Address)
	}
	id, err := messageID(from.Address)
	if err != nil {
		return err
	}
	body := BuildMIME(m, time.Now(), id)

	deadline := time.Now().Add(p.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	return p.deliver(ctx, deadline, from.Address, rcpts, body)
}

func (p *Provider) deliver(ctx context.Context, deadline time.Time, from string, rcpts []string, body []byte) error {
	addr := net.JoinHostPort(p.host, strconv.Itoa(p.port))
	tlsCfg := p.tlsConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tlsCfg = tlsCfg.Clone()
	if tlsCfg.ServerName == "" {
		tlsCfg.ServerName = p.host
	}

	nd := &net.Dialer{Deadline: deadline}
	var conn net.Conn
	var err error
	if p.tlsMode == "implicit" {
		conn, err = (&tls.Dialer{NetDialer: nd, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = nd.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return stageErr("dial", fmt.Errorf("smtp: dial: %w", err))
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(deadline); err != nil {
		return stageErr("dial", fmt.Errorf("smtp: set deadline: %w", err))
	}
	// Abort a blocked exchange when the caller cancels.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	c, err := netsmtp.NewClient(conn, p.host)
	if err != nil {
		return stageErr("greeting", fmt.Errorf("smtp: greeting: %w", err))
	}
	defer func() { _ = c.Close() }()

	if p.tlsMode == "starttls" {
		if err := c.StartTLS(tlsCfg); err != nil {
			return stageErr("tls", fmt.Errorf("smtp: starttls: %w", err))
		}
	}
	if p.username != "" {
		if err := c.Auth(netsmtp.PlainAuth("", p.username, p.password, p.host)); err != nil {
			return stageErr("auth", fmt.Errorf("smtp: auth: %w", err))
		}
	}
	if err := c.Mail(from); err != nil {
		return stageErr("mail", fmt.Errorf("smtp: mail from: %w", err))
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return stageErr("rcpt", fmt.Errorf("smtp: rcpt to: %w", err))
		}
	}
	w, err := c.Data()
	if err != nil {
		return stageErr("data", fmt.Errorf("smtp: data: %w", err))
	}
	if _, err := w.Write(body); err != nil {
		return stageErr("data", fmt.Errorf("smtp: write body: %w", err))
	}
	if err := w.Close(); err != nil {
		return stageErr("data", fmt.Errorf("smtp: end data: %w", err))
	}
	// The server has accepted the message; a failing QUIT must not turn a
	// delivered message into an error (it would trigger a duplicate send).
	_ = c.Quit()
	return nil
}

// stageErr tags err with the SMTP stage and, when the server answered, its
// numeric reply code. The server text stays only in the wrapped error.
func stageErr(stage string, err error) error {
	se := &mail.SendError{Stage: stage, Err: err}
	var tp *textproto.Error
	if errors.As(err, &tp) {
		se.Code = tp.Code
	}
	return se
}

func messageID(fromAddr string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("smtp: message id: %w", err)
	}
	domain := "localhost"
	if i := strings.LastIndexByte(fromAddr, '@'); i >= 0 && i+1 < len(fromAddr) {
		domain = fromAddr[i+1:]
	}
	return "<" + hex.EncodeToString(b[:]) + "@" + domain + ">", nil
}

// oneLine strips CR and LF so a value cannot start a new header.
func oneLine(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(s)
}

// addrHeader renders addresses with RFC 2047-encoded display names. Values
// that do not parse as a single address are dropped, never passed through.
func addrHeader(raw ...string) string {
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		a, err := stdmail.ParseAddress(r)
		if err != nil {
			continue
		}
		if a.Name == "" {
			out = append(out, a.Address)
		} else {
			out = append(out, a.String())
		}
	}
	return strings.Join(out, ", ")
}

// BuildMIME renders m as a multipart/alternative message (text then HTML),
// both parts UTF-8 quoted-printable, with CRLF line endings.
func BuildMIME(m mail.Message, now time.Time, messageID string) []byte {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	// The multipart body is built first so the boundary is known for the header.
	for _, p := range []struct{ ctype, content string }{
		{"text/plain", m.Text},
		{"text/html", m.HTML},
	} {
		pw, _ := mw.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {p.ctype + "; charset=UTF-8"},
			"Content-Transfer-Encoding": {"quoted-printable"},
		})
		qw := quotedprintable.NewWriter(pw)
		_, _ = io.WriteString(qw, p.content)
		_ = qw.Close()
	}
	_ = mw.Close()

	var out bytes.Buffer
	hdr := func(k, v string) { out.WriteString(k + ": " + v + "\r\n") }
	hdr("From", addrHeader(m.From))
	hdr("To", addrHeader(m.To...))
	if rt := addrHeader(m.ReplyTo); rt != "" {
		hdr("Reply-To", rt)
	}
	hdr("Subject", mime.QEncoding.Encode("UTF-8", oneLine(m.Subject)))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", oneLine(messageID))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", "multipart/alternative; boundary="+mw.Boundary())
	out.WriteString("\r\n")
	out.Write(buf.Bytes())
	return out.Bytes()
}
