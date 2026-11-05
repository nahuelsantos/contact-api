package render_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/forms"
	"github.com/nahuelsantos/contact-api/internal/render"
)

var update = flag.Bool("update", false, "rewrite golden files")

func site() *config.Site { return &config.Site{Name: "example", From: "Example <noreply@example.com>"} }

func form(subject string) *config.Form {
	return &config.Form{Name: "contact", Subject: subject}
}

func vals(name, message string) forms.Values {
	return forms.Values{
		{Name: "name", Label: "Name", Type: "text", Value: name},
		{Name: "email", Label: "Email", Type: "email", Value: "ana@example.com"},
		{Name: "subject", Label: "Subject", Type: "text", Value: "Hello"},
		{Name: "message", Label: "Message", Type: "textarea", Value: message},
	}
}

func newR(t *testing.T, dir string) *render.Renderer {
	t.Helper()
	r, err := render.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path) //nolint:gosec // fixed testdata path
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("%s mismatch (run with -update)\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestNotification_Golden(t *testing.T) {
	e, err := newR(t, "").Notification(site(), form("New message from {{name}}"), vals("Ana", "Line one\nLine two"))
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "notification.html.golden", e.HTML)
	golden(t, "notification.txt.golden", e.Text)
	if e.Subject != "New message from Ana" {
		t.Fatalf("subject %q", e.Subject)
	}
}

func TestNotification_EscapesHTML(t *testing.T) {
	e, err := newR(t, "").Notification(site(), form(""), vals("Ana", "<script>alert(1)</script>"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "&lt;script&gt;") || strings.Contains(e.HTML, "<script>") {
		t.Fatalf("not escaped: %s", e.HTML)
	}
	if !strings.Contains(e.Text, "<script>alert(1)</script>") {
		t.Fatalf("text should be raw: %s", e.Text)
	}
}

func TestNotification_Accents(t *testing.T) {
	const name = "Ñandú Pérez 🙂"
	e, err := newR(t, "").Notification(site(), form(""), vals(name, "hola"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, name) || !strings.Contains(e.Text, name) {
		t.Fatalf("name altered:\n%s\n%s", e.HTML, e.Text)
	}
}

func TestNotification_PreservesLineBreaks(t *testing.T) {
	e, err := newR(t, "").Notification(site(), form(""), vals("Ana", "a\r\nb\nc"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "a<br>b<br>c") {
		t.Fatalf("html: %s", e.HTML)
	}
	if !strings.Contains(e.Text, "a\nb\nc") || strings.Contains(e.Text, "\r") {
		t.Fatalf("text: %q", e.Text)
	}
}

func TestNotification_OmitsEmptyOptionalFields(t *testing.T) {
	v := append(vals("Ana", "hi"), forms.Value{Name: "phone", Label: "Phone", Type: "text", Value: ""})
	e, err := newR(t, "").Notification(site(), form(""), v)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.HTML, "Phone") || strings.Contains(e.Text, "Phone") {
		t.Fatalf("empty field shown:\n%s\n%s", e.HTML, e.Text)
	}
}

func TestSubject_StripsCRLF(t *testing.T) {
	v := forms.Values{{Name: "subject", Value: "hi\r\nBcc: a@b.c"}}
	got := render.Subject("[x] {{subject}}", v)
	if got != "[x] hi Bcc: a@b.c" {
		t.Fatalf("got %q", got)
	}
	long := forms.Values{{Name: "subject", Value: strings.Repeat("é", 300) + "\r"}}
	got = render.Subject("{{subject}}", long)
	if n := utf8.RuneCountInString(got); n != 200 {
		t.Fatalf("runes %d", n)
	}
	if strings.ContainsAny(render.Subject("a\r{{subject}}\n", v), "\r\n") {
		t.Fatal("CR/LF survived")
	}
}

func TestSubject_UnknownPlaceholderEmpty(t *testing.T) {
	if got := render.Subject("a {{nope}} b", nil); got != "a  b" {
		t.Fatalf("got %q", got)
	}
}

func TestSubject_DefaultWhenEmpty(t *testing.T) {
	e, err := newR(t, "").Notification(site(), form(""), vals("Ana", "hi"))
	if err != nil {
		t.Fatal(err)
	}
	if e.Subject != "[example] contact" {
		t.Fatalf("got %q", e.Subject)
	}
}

func TestAutoReply_SubstitutesAndEscapes(t *testing.T) {
	f := form("")
	f.AutoReply = &config.AutoReply{Subject: "Gracias {{nombre}}", Body: "Hola {{nombre}}\nChau"}
	v := forms.Values{{Name: "nombre", Label: "Nombre", Type: "text", Value: "<b>Ana</b>"}}
	e, err := newR(t, "").AutoReply(site(), f, v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "Hola &lt;b&gt;Ana&lt;/b&gt;<br>Chau") {
		t.Fatalf("html: %s", e.HTML)
	}
	if !strings.Contains(e.Text, "Hola <b>Ana</b>\nChau") {
		t.Fatalf("text: %q", e.Text)
	}
	if e.Subject != "Gracias <b>Ana</b>" {
		t.Fatalf("subject %q", e.Subject)
	}
}

func TestAutoReply_NoConfig(t *testing.T) {
	if _, err := newR(t, "").AutoReply(site(), form(""), nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestNew_LoadsCustomTemplate(t *testing.T) {
	dir := t.TempDir()
	write := func(n, s string) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("fancy.html.tmpl", "<p>FANCY {{.Site}}/{{.Form}} {{range .Fields}}{{.Value}};{{end}}</p>")
	write("fancy.txt.tmpl", "FANCY {{.Site}}/{{.Form}}")
	r := newR(t, dir)
	if !r.Has("fancy") || !r.Has("default") || r.Has("nope") || r.Has("../fancy") {
		t.Fatal("Has mismatch")
	}
	f := form("")
	f.Template = "fancy"
	e, err := r.Notification(site(), f, vals("<i>", "m"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.HTML, "FANCY example/contact") || !strings.Contains(e.HTML, "&lt;i&gt;;") || e.Text != "FANCY example/contact" {
		t.Fatalf("html %q text %q", e.HTML, e.Text)
	}
}

func TestNew_BadTemplateDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.html.tmpl"), []byte("{{.Oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := render.New(dir); err == nil {
		t.Fatal("expected parse error")
	}
}
