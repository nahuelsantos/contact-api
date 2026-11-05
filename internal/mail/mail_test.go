package mail_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/mail"
)

type fake struct {
	name  string
	err   error
	calls int
}

func (f *fake) Name() string { return f.name }

func (f *fake) Send(context.Context, mail.Message) error {
	f.calls++
	return f.err
}

func sentBy(t *testing.T, p mail.Provider) string {
	t.Helper()
	s, ok := p.(mail.SentBy)
	if !ok {
		t.Fatalf("%T does not implement mail.SentBy", p)
	}
	return s.SentBy()
}

func TestFallback_PrimaryOK(t *testing.T) {
	primary, fallback := &fake{name: "a"}, &fake{name: "b"}
	p := mail.WithFallback(primary, fallback)
	if err := p.Send(context.Background(), mail.Message{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fallback.calls != 0 || primary.calls != 1 {
		t.Fatalf("calls primary=%d fallback=%d", primary.calls, fallback.calls)
	}
	if got := sentBy(t, p); got != "a" {
		t.Fatalf("SentBy = %q", got)
	}
}

func TestFallback_PrimaryFails(t *testing.T) {
	primary := &fake{name: "a", err: errors.New("boom")}
	fallback := &fake{name: "b"}
	p := mail.WithFallback(primary, fallback)
	if err := p.Send(context.Background(), mail.Message{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback calls = %d", fallback.calls)
	}
	if got := sentBy(t, p); got != "b" {
		t.Fatalf("SentBy = %q", got)
	}
}

func TestFallback_BothFail(t *testing.T) {
	e1, e2 := errors.New("one"), errors.New("two")
	p := mail.WithFallback(&fake{name: "a", err: e1}, &fake{name: "b", err: e2})
	err := p.Send(context.Background(), mail.Message{})
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("error does not wrap both: %v", err)
	}
}

func TestFallback_NilReturnsPrimary(t *testing.T) {
	primary := &fake{name: "a"}
	if got := mail.WithFallback(primary, nil); got != mail.Provider(primary) {
		t.Fatalf("expected primary itself, got %T", got)
	}
}

func TestBuild_UnknownType(t *testing.T) {
	_, err := mail.Build(map[string]config.ProviderConfig{"x": {Type: "nope"}}, map[string]mail.Factory{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuild_UsesFactory(t *testing.T) {
	f := func(name string, _ config.ProviderConfig) (mail.Provider, error) { return &fake{name: name}, nil }
	got, err := mail.Build(map[string]config.ProviderConfig{"x": {Type: "fake"}}, map[string]mail.Factory{"fake": f})
	if err != nil {
		t.Fatal(err)
	}
	if got["x"] == nil || got["x"].Name() != "x" {
		t.Fatalf("got %+v", got)
	}
}
