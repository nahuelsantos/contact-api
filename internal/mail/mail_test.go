package mail_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/mail"
)

type fake struct {
	name  string
	err   error
	calls atomic.Int32
	// failOnSubject makes Send fail when Message.Subject == "fail".
	failOnSubject bool
	onSend        func()
}

func (f *fake) Name() string { return f.name }

func (f *fake) Send(_ context.Context, m mail.Message) error {
	f.calls.Add(1)
	if f.onSend != nil {
		f.onSend()
	}
	if f.failOnSubject && m.Subject == "fail" {
		return errFail
	}
	return f.err
}

var errFail = errors.New("forced failure")

func TestFallback_PrimaryOK(t *testing.T) {
	primary, fallback := &fake{name: "a"}, &fake{name: "b"}
	p := mail.WithFallback(primary, fallback)
	res, err := mail.Deliver(context.Background(), p, mail.Message{})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if fallback.calls.Load() != 0 || primary.calls.Load() != 1 {
		t.Fatalf("calls primary=%d fallback=%d", primary.calls.Load(), fallback.calls.Load())
	}
	if !reflect.DeepEqual(res, mail.Result{SentBy: "a"}) {
		t.Fatalf("res = %+v", res)
	}
}

func TestFallback_PrimaryFails(t *testing.T) {
	primary := &fake{name: "a", err: errors.New("boom")}
	fallback := &fake{name: "b"}
	p := mail.WithFallback(primary, fallback)
	res, err := mail.Deliver(context.Background(), p, mail.Message{})
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if fallback.calls.Load() != 1 {
		t.Fatalf("fallback calls = %d", fallback.calls.Load())
	}
	if res.SentBy != "b" || !reflect.DeepEqual(res.Failed, []string{"a"}) {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Errors) != 1 || !errors.Is(res.Errors[0], primary.err) {
		t.Fatalf("primary error not kept: %+v", res.Errors)
	}
	if err := p.Send(context.Background(), mail.Message{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if p.Name() != "a" {
		t.Fatalf("Name = %q", p.Name())
	}
}

func TestFallback_BothFail(t *testing.T) {
	e1, e2 := errors.New("one"), errors.New("two")
	p := mail.WithFallback(&fake{name: "a", err: e1}, &fake{name: "b", err: e2})
	res, err := mail.Deliver(context.Background(), p, mail.Message{})
	if !errors.Is(err, e1) || !errors.Is(err, e2) {
		t.Fatalf("error does not wrap both: %v", err)
	}
	if res.SentBy != "" || !reflect.DeepEqual(res.Failed, []string{"a", "b"}) {
		t.Fatalf("res = %+v", res)
	}
	if len(res.Errors) != 2 || !errors.Is(res.Errors[0], e1) || !errors.Is(res.Errors[1], e2) {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestDeliver_PlainProvider(t *testing.T) {
	res, err := mail.Deliver(context.Background(), &fake{name: "a"}, mail.Message{})
	if err != nil || !reflect.DeepEqual(res, mail.Result{SentBy: "a"}) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	boom := errors.New("boom")
	res, err = mail.Deliver(context.Background(), &fake{name: "a", err: boom}, mail.Message{})
	if !errors.Is(err, boom) || res.SentBy != "" || !reflect.DeepEqual(res.Failed, []string{"a"}) {
		t.Fatalf("res = %+v err = %v", res, err)
	}
	if len(res.Errors) != 1 || !errors.Is(res.Errors[0], boom) {
		t.Fatalf("errors = %+v", res.Errors)
	}
}

func TestFallback_CanceledSkipsFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	primary := &fake{name: "a", err: errors.New("boom"), onSend: cancel}
	fallback := &fake{name: "b"}
	res, err := mail.Deliver(ctx, mail.WithFallback(primary, fallback), mail.Message{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error should wrap ctx error: %v", err)
	}
	if fallback.calls.Load() != 0 {
		t.Fatal("fallback was called after cancellation")
	}
	if !reflect.DeepEqual(res.Failed, []string{"a"}) || res.SentBy != "" {
		t.Fatalf("res = %+v", res)
	}
}

func TestFallback_ConcurrentAttribution(t *testing.T) {
	p := mail.WithFallback(&fake{name: "a", failOnSubject: true}, &fake{name: "b"})
	var wg sync.WaitGroup
	errs := make(chan string, 50)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			subject, want := "ok", "a"
			if i%2 == 0 {
				subject, want = "fail", "b"
			}
			res, err := mail.Deliver(context.Background(), p, mail.Message{Subject: subject})
			if err != nil || res.SentBy != want {
				errs <- fmt.Sprintf("goroutine %d: SentBy=%q want %q err=%v", i, res.SentBy, want, err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
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

type timeoutErr struct{}

func (timeoutErr) Error() string { return "i/o timeout" }
func (timeoutErr) Timeout() bool { return true }

func TestErrorClass(t *testing.T) {
	leak := errors.New("550 MARK-SERVER-TEXT <visitor@leak.test> unknown")
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"http status", &mail.SendError{Stage: "status", Code: 502, Err: leak}, "status_502"},
		{"smtp rcpt", &mail.SendError{Stage: "rcpt", Code: 550, Err: leak}, "rcpt_550"},
		{"wrapped", fmt.Errorf("deliver: %w", &mail.SendError{Stage: "auth", Code: 535, Err: leak}), "auth_535"},
		{"stage timeout", &mail.SendError{Stage: "dial", Err: timeoutErr{}}, "dial_timeout"},
		{"stage deadline", &mail.SendError{Stage: "request", Err: context.DeadlineExceeded}, "request_timeout"},
		{"stage canceled", &mail.SendError{Stage: "data", Err: context.Canceled}, "data_canceled"},
		{"stage only", &mail.SendError{Stage: "dial", Err: leak}, "dial"},
		{"bare deadline", context.DeadlineExceeded, "timeout"},
		{"bare canceled", context.Canceled, "canceled"},
		{"unknown", leak, "error"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mail.ErrorClass(tc.err); got != tc.want {
				t.Fatalf("ErrorClass = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSendError_UnwrapsAndKeepsMessage(t *testing.T) {
	inner := errors.New("inner")
	err := error(&mail.SendError{Stage: "dial", Err: inner})
	if !errors.Is(err, inner) || err.Error() != "inner" {
		t.Fatalf("err = %v", err)
	}
}
