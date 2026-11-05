package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	stdmail "net/mail"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/mail"
)

var _ mail.Provider = (*Provider)(nil)

var testNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

func sampleMessage() mail.Message {
	return mail.Message{
		From:    "Red Analítica <admisiones@example.org>",
		ReplyTo: "visitor@example.com",
		To:      []string{"Admisiones <a@example.org>", "b@example.org"},
		Subject: "Nueva admisión: Ñandú",
		Text:    "Hola ñandú\nsegunda línea",
		HTML:    "<p>Hola <b>ñandú</b></p>",
	}
}

func TestBuildMIME_Structure(t *testing.T) {
	raw := BuildMIME(sampleMessage(), testNow, "<abc@example.org>")
	msg, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	h := msg.Header
	for k, want := range map[string]string{
		"Mime-Version": "1.0",
		"Message-Id":   "<abc@example.org>",
		"Reply-To":     "visitor@example.com",
		"Date":         testNow.Format(time.RFC1123Z),
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	dec := new(mime.WordDecoder)
	from, err := stdmail.ParseAddress(h.Get("From"))
	if err != nil || from.Address != "admisiones@example.org" || from.Name != "Red Analítica" {
		t.Errorf("From = %q (%v)", h.Get("From"), err)
	}
	if !strings.Contains(h.Get("From"), "=?") {
		t.Errorf("From display name not RFC 2047 encoded: %q", h.Get("From"))
	}
	to, err := stdmail.ParseAddressList(h.Get("To"))
	if err != nil || len(to) != 2 || to[0].Address != "a@example.org" || to[1].Address != "b@example.org" {
		t.Errorf("To = %q (%v)", h.Get("To"), err)
	}
	if s, _ := dec.DecodeHeader(h.Get("Subject")); s != "Nueva admisión: Ñandú" {
		t.Errorf("Subject decoded = %q", s)
	}

	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("Content-Type = %q (%v)", h.Get("Content-Type"), err)
	}
	if n := strings.Count(string(raw), "Content-Transfer-Encoding: quoted-printable"); n != 2 {
		t.Errorf("quoted-printable declared %d times, want 2", n)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	wantTypes := []string{"text/plain", "text/html"}
	wantBodies := []string{sampleMessage().Text, sampleMessage().HTML}
	for i := range wantTypes {
		part, err := mr.NextPart()
		if err != nil {
			t.Fatalf("part %d: %v", i, err)
		}
		pt, pp, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if pt != wantTypes[i] || !strings.EqualFold(pp["charset"], "UTF-8") {
			t.Errorf("part %d type = %q %v", i, pt, pp)
		}
		// multipart.Reader decodes quoted-printable itself and drops the header.
		body, err := io.ReadAll(part)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.ReplaceAll(string(body), "\r\n", "\n"); got != wantBodies[i] {
			t.Errorf("part %d body = %q, want %q", i, got, wantBodies[i])
		}
	}
	if _, err := mr.NextPart(); !errors.Is(err, io.EOF) {
		t.Errorf("expected exactly two parts, got %v", err)
	}
}

func TestBuildMIME_EncodesNonASCIISubject(t *testing.T) {
	raw := string(BuildMIME(sampleMessage(), testNow, "<a@b>"))
	if !strings.Contains(raw, "Subject: =?UTF-8?q?") {
		t.Fatalf("subject not q-encoded:\n%s", raw)
	}
	msg, err := stdmail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || got != "Nueva admisión: Ñandú" {
		t.Errorf("decoded = %q (%v)", got, err)
	}
}

func TestBuildMIME_NoReplyToWhenEmpty(t *testing.T) {
	m := sampleMessage()
	m.ReplyTo = ""
	raw := string(BuildMIME(m, testNow, "<a@b>"))
	if strings.Contains(raw, "Reply-To") {
		t.Errorf("unexpected Reply-To header:\n%s", raw)
	}
}

func TestBuildMIME_SubjectCannotInjectHeaders(t *testing.T) {
	m := sampleMessage()
	m.Subject = "hi\r\nBcc: evil@example.com"
	raw := BuildMIME(m, testNow, "<a@b>")
	msg, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Header.Get("Bcc") != "" {
		t.Error("header injection through subject")
	}
}

// --- in-process server ---

type received struct {
	from string
	rcpt []string
	data []byte
	user string
	pass string
}

type testBackend struct {
	insecureAuth bool
	mu           sync.Mutex
	got          []received
	wantAuth     bool
}

func (b *testBackend) NewSession(*gosmtp.Conn) (gosmtp.Session, error) {
	return &testSession{b: b}, nil
}

type testSession struct {
	b    *testBackend
	cur  received
	auth bool
}

func (s *testSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *testSession) Auth(string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, user, pass string) error {
		if user != "u" || pass != "p" {
			return gosmtp.ErrAuthFailed
		}
		s.auth = true
		s.cur.user, s.cur.pass = user, pass
		return nil
	}), nil
}

func (s *testSession) Mail(from string, _ *gosmtp.MailOptions) error {
	if s.b.wantAuth && !s.auth {
		return gosmtp.ErrAuthRequired
	}
	s.cur.from = from
	return nil
}

func (s *testSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.cur.rcpt = append(s.cur.rcpt, to)
	return nil
}

func (s *testSession) Data(r io.Reader) error {
	d, err := io.ReadAll(r)
	if err != nil {
		return err //nolint:wrapcheck // test double
	}
	s.cur.data = d
	s.b.mu.Lock()
	s.b.got = append(s.b.got, s.cur)
	s.b.mu.Unlock()
	return nil
}

func (s *testSession) Reset()        { s.cur = received{user: s.cur.user, pass: s.cur.pass} }
func (s *testSession) Logout() error { return nil }

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

func startServer(t *testing.T, b *testBackend, tlsCfg *tls.Config, implicit bool) (host string, port int) {
	t.Helper()
	srv := gosmtp.NewServer(b)
	srv.Domain = "localhost"
	srv.TLSConfig = tlsCfg
	srv.AllowInsecureAuth = b.insecureAuth
	srv.ReadTimeout, srv.WriteTimeout = 5*time.Second, 5*time.Second
	ln := listen(t)
	if implicit {
		ln = tls.NewListener(ln, tlsCfg)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	a := ln.Addr().(*net.TCPAddr)
	return "127.0.0.1", a.Port
}

func selfSigned(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
}

func checkDelivered(t *testing.T, b *testBackend) received {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.got) != 1 {
		t.Fatalf("server received %d messages, want 1", len(b.got))
	}
	r := b.got[0]
	if r.from != "admisiones@example.org" {
		t.Errorf("MAIL FROM = %q", r.from)
	}
	if len(r.rcpt) != 2 || r.rcpt[0] != "a@example.org" || r.rcpt[1] != "b@example.org" {
		t.Errorf("RCPT TO = %v", r.rcpt)
	}
	msg, err := stdmail.ReadMessage(bytes.NewReader(r.data))
	if err != nil {
		t.Fatal(err)
	}
	if mt, _, _ := mime.ParseMediaType(msg.Header.Get("Content-Type")); mt != "multipart/alternative" {
		t.Errorf("Content-Type = %q", msg.Header.Get("Content-Type"))
	}
	if id := msg.Header.Get("Message-Id"); !strings.HasPrefix(id, "<") || !strings.HasSuffix(id, "@example.org>") {
		t.Errorf("Message-ID = %q", id)
	}
	return r
}

func TestSend_Plain(t *testing.T) {
	b := &testBackend{}
	host, port := startServer(t, b, nil, false)
	// TLS "" must behave as "none".
	p := New("melee", config.ProviderConfig{Type: "smtp", Host: host, Port: port})
	if p.Name() != "melee" {
		t.Errorf("Name = %q", p.Name())
	}
	if err := p.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	checkDelivered(t, b)
}

func TestSend_StartTLSWithAuth(t *testing.T) {
	b := &testBackend{wantAuth: true}
	cfg := selfSigned(t)
	host, port := startServer(t, b, cfg, false)
	p := New("relay", config.ProviderConfig{
		Type: "smtp", Host: host, Port: port, TLS: "starttls", Username: "u", Password: "p",
	}, withTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})) //nolint:gosec // self-signed test cert
	if err := p.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if r := checkDelivered(t, b); r.user != "u" {
		t.Errorf("not authenticated, user = %q", r.user)
	}
}

func TestSend_ImplicitTLS(t *testing.T) {
	b := &testBackend{}
	host, port := startServer(t, b, selfSigned(t), true)
	p := New("relay", config.ProviderConfig{Type: "smtp", Host: host, Port: port, TLS: "implicit"},
		withTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})) //nolint:gosec // self-signed test cert
	if err := p.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	checkDelivered(t, b)
}

func TestSend_BadAuth(t *testing.T) {
	b := &testBackend{wantAuth: true}
	host, port := startServer(t, b, selfSigned(t), false)
	p := New("relay", config.ProviderConfig{
		Type: "smtp", Host: host, Port: port, TLS: "starttls", Username: "u", Password: "wrong",
	}, withTLSConfig(&tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12})) //nolint:gosec // self-signed test cert
	if err := p.Send(context.Background(), sampleMessage()); err == nil {
		t.Fatal("expected auth failure")
	}
}

func TestSend_InvalidFrom(t *testing.T) {
	p := New("x", config.ProviderConfig{Host: "127.0.0.1", Port: 1})
	m := sampleMessage()
	m.From = "not an address"
	if err := p.Send(context.Background(), m); !errors.Is(err, ErrAddress) {
		t.Errorf("err = %v, want ErrAddress", err)
	}
}

func TestSend_UnknownTLSMode(t *testing.T) {
	p := New("x", config.ProviderConfig{Host: "127.0.0.1", Port: 1, TLS: "bogus"})
	if err := p.Send(context.Background(), sampleMessage()); !errors.Is(err, ErrTLSMode) {
		t.Errorf("err = %v, want ErrTLSMode", err)
	}
}

func TestSend_Timeout(t *testing.T) {
	ln := listen(t)
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			defer func() { _ = c.Close() }()
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	p := New("slow", config.ProviderConfig{Type: "smtp", Host: "127.0.0.1", Port: a.Port}, withTimeout(200*time.Millisecond))
	start := time.Now()
	err := p.Send(context.Background(), sampleMessage())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestSend_ContextDeadlineShorterThanDefault(t *testing.T) {
	ln := listen(t)
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			defer func() { _ = c.Close() }()
			time.Sleep(5 * time.Second)
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	p := New("slow", config.ProviderConfig{Host: "127.0.0.1", Port: a.Port})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.Send(ctx, sampleMessage()); err == nil {
		t.Fatal("expected error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("took %v", d)
	}
}

func TestSend_PlainWithAuthOnLocalhost(t *testing.T) {
	b := &testBackend{wantAuth: true, insecureAuth: true}
	host, port := startServer(t, b, nil, false)
	p := New("melee", config.ProviderConfig{Host: host, Port: port, Username: "u", Password: "p"})
	if err := p.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if r := checkDelivered(t, b); r.user != "u" {
		t.Errorf("user = %q", r.user)
	}
}

func TestSend_QuitFailureAfterAcceptanceIsSuccess(t *testing.T) {
	ln := listen(t)
	defer func() { _ = ln.Close() }()
	accepted := make(chan struct{})
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		r := bufio.NewReader(c)
		say := func(s string) { _, _ = io.WriteString(c, s+"\r\n") }
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch strings.ToUpper(strings.TrimSpace(line))[:4] {
			case "EHLO":
				say("250 fake")
			case "MAIL", "RCPT":
				say("250 ok")
			case "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
				}
				say("250 queued")
				close(accepted)
			case "QUIT":
				return // drop without 221
			default:
				say("500 what")
			}
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	p := New("fake", config.ProviderConfig{Host: "127.0.0.1", Port: a.Port})
	if err := p.Send(context.Background(), sampleMessage()); err != nil {
		t.Fatalf("Send = %v, want nil after server acceptance", err)
	}
	select {
	case <-accepted:
	default:
		t.Error("server never accepted the message")
	}
}

func TestSend_InvalidReplyToRejectedBeforeSending(t *testing.T) {
	for _, rt := range []string{"a@x.com, evil@y.com", "a@x.com\r\nBcc: evil@y.com", "bad\xffaddr@x.com", "nonsense"} {
		b := &testBackend{}
		host, port := startServer(t, b, nil, false)
		p := New("x", config.ProviderConfig{Host: host, Port: port})
		m := sampleMessage()
		m.ReplyTo = rt
		if err := p.Send(context.Background(), m); !errors.Is(err, ErrAddress) {
			t.Errorf("ReplyTo %q: err = %v, want ErrAddress", rt, err)
		}
		b.mu.Lock()
		n := len(b.got)
		b.mu.Unlock()
		if n != 0 {
			t.Errorf("ReplyTo %q: %d messages sent", rt, n)
		}
	}
}

func TestBuildMIME_OmitsUnparseableReplyTo(t *testing.T) {
	m := sampleMessage()
	m.ReplyTo = "a@x.com, evil@y.com"
	if raw := string(BuildMIME(m, testNow, "<a@b>")); strings.Contains(raw, "Reply-To") || strings.Contains(raw, "evil") {
		t.Errorf("unparseable Reply-To emitted:\n%s", raw)
	}
}
