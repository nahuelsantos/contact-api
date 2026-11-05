// Package render builds notification and auto-reply emails from validated submissions.
package render

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	texttemplate "text/template"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/forms"
)

//go:embed templates/*
var embedded embed.FS

const (
	defaultName   = "default"
	maxSubjectLen = 200
	htmlSuffix    = ".html.tmpl"
	txtSuffix     = ".txt.tmpl"
)

// ErrNoAutoReply is returned when AutoReply is called for a form without auto_reply.
var ErrNoAutoReply = errors.New("form has no auto_reply")

var newlineRe = regexp.MustCompile(`\r\n|\r|\n`)

// Email is a rendered message ready for a mail provider.
type Email struct{ Subject, HTML, Text string }

type pair struct {
	html *htmltemplate.Template
	txt  *texttemplate.Template
}

// Renderer renders emails from embedded and optional on-disk templates.
type Renderer struct {
	sets      map[string]pair
	autoReply *htmltemplate.Template
}

type data struct {
	Site, Form string
	Fields     []forms.Value
}

func lines(s string) []string { return newlineRe.Split(s, -1) }

func text(s string) string { return newlineRe.ReplaceAllString(s, "\n") }

var (
	htmlFuncs = htmltemplate.FuncMap{"lines": lines}
	textFuncs = texttemplate.FuncMap{"lines": lines, "text": text}
)

// New loads the embedded templates and, if templatesDir is set, every
// <name>.html.tmpl / <name>.txt.tmpl pair found there.
func New(templatesDir string) (*Renderer, error) {
	r := &Renderer{sets: map[string]pair{}}

	h, err := htmltemplate.New(defaultName+htmlSuffix).Funcs(htmlFuncs).ParseFS(embedded, "templates/"+defaultName+htmlSuffix)
	if err != nil {
		return nil, fmt.Errorf("parse embedded html: %w", err)
	}
	t, err := texttemplate.New(defaultName+txtSuffix).Funcs(textFuncs).ParseFS(embedded, "templates/"+defaultName+txtSuffix)
	if err != nil {
		return nil, fmt.Errorf("parse embedded text: %w", err)
	}
	r.sets[defaultName] = pair{h, t}

	r.autoReply, err = htmltemplate.New("autoreply"+htmlSuffix).Funcs(htmlFuncs).ParseFS(embedded, "templates/autoreply"+htmlSuffix)
	if err != nil {
		return nil, fmt.Errorf("parse embedded auto-reply: %w", err)
	}

	if templatesDir != "" {
		if err := r.loadDir(templatesDir); err != nil {
			return nil, err
		}
	}

	return r, nil
}

func (r *Renderer) loadDir(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read templates dir: %w", err)
	}
	for _, e := range entries {
		file := e.Name()
		name, ok := strings.CutSuffix(file, htmlSuffix)
		if !ok || e.IsDir() {
			continue
		}
		htmlPath := filepath.Join(dir, file)
		txtPath := filepath.Join(dir, name+txtSuffix)
		h, err := htmltemplate.New(file).Funcs(htmlFuncs).ParseFiles(htmlPath)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", file, err)
		}
		t, err := texttemplate.New(name + txtSuffix).Funcs(textFuncs).ParseFiles(txtPath)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", name+txtSuffix, err)
		}
		r.sets[name] = pair{h, t}
	}

	return nil
}

// Has reports whether a template with the given name is available.
func (r *Renderer) Has(name string) bool {
	_, ok := r.sets[name]

	return ok
}

// Subject fills {{field}} placeholders from v, replaces CR/LF with spaces,
// trims, and caps the result at 200 runes. Unknown placeholders become "".
func Subject(tmpl string, v forms.Values) string {
	s := config.ReplacePlaceholders(tmpl, v.Get)
	s = strings.TrimSpace(newlineRe.ReplaceAllString(s, " "))
	if rs := []rune(s); len(rs) > maxSubjectLen {
		s = strings.TrimSpace(string(rs[:maxSubjectLen]))
	}

	return s
}

func defaultSubject(s *config.Site, f *config.Form) string {
	return Subject("["+s.Name+"] "+f.Name, nil)
}

// Notification renders the email sent to the site owner.
func (r *Renderer) Notification(s *config.Site, f *config.Form, v forms.Values) (Email, error) {
	name := f.Template
	if name == "" {
		name = defaultName
	}
	set, ok := r.sets[name]
	if !ok {
		return Email{}, fmt.Errorf("render: unknown template %q", name) //nolint:err113 // includes the offending name
	}

	d := data{Site: s.Name, Form: f.Name}
	for _, x := range v {
		if x.Value != "" {
			d.Fields = append(d.Fields, x)
		}
	}

	var hb bytes.Buffer
	if err := set.html.Execute(&hb, d); err != nil {
		return Email{}, fmt.Errorf("render html: %w", err)
	}
	var tb bytes.Buffer
	if err := set.txt.Execute(&tb, d); err != nil {
		return Email{}, fmt.Errorf("render text: %w", err)
	}

	subj := Subject(f.Subject, v)
	if subj == "" {
		subj = defaultSubject(s, f)
	}

	return Email{Subject: subj, HTML: hb.String(), Text: tb.String()}, nil
}

// AutoReply renders the confirmation email sent to the submitter.
func (r *Renderer) AutoReply(s *config.Site, f *config.Form, v forms.Values) (Email, error) {
	if f.AutoReply == nil {
		return Email{}, ErrNoAutoReply
	}
	body := config.ReplacePlaceholders(f.AutoReply.Body, v.Get)

	var hb bytes.Buffer
	if err := r.autoReply.Execute(&hb, struct{ Site, Body string }{s.Name, body}); err != nil {
		return Email{}, fmt.Errorf("render auto-reply: %w", err)
	}

	subj := Subject(f.AutoReply.Subject, v)
	if subj == "" {
		subj = defaultSubject(s, f)
	}

	return Email{Subject: subj, HTML: hb.String(), Text: text(body)}, nil
}
