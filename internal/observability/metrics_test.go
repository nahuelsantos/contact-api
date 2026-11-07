package observability_test

import (
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/observability"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMetrics_Names(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := observability.NewMetrics(reg)

	m.Submission("example", "contact", "sent")
	m.Submission("example", "contact", "sent")
	m.Submission("example", "contact", "rate_limited")
	m.SendDuration("resend", 250*time.Millisecond)
	m.SendDuration("resend", 2*time.Second)
	m.ProviderFailure("smtp")
	m.AutoReplyFailure("example", "contact")

	counters := []struct {
		name   string
		metric string
		want   float64
	}{
		{"sent", `contact_submissions_total{form="contact",outcome="sent",site="example"}`, 2},
		{"rate_limited", `contact_submissions_total{form="contact",outcome="rate_limited",site="example"}`, 1},
		{"provider failure", `contact_provider_failures_total{provider="smtp"}`, 1},
		{"autoreply failure", `contact_autoreply_failures_total{form="contact",site="example"}`, 1},
	}
	for _, c := range counters {
		got := gatherValue(t, reg, c.metric)
		if got != c.want {
			t.Errorf("%s = %v, want %v", c.metric, got, c.want)
		}
	}

	if n, err := testutil.GatherAndCount(reg, "contact_send_duration_seconds"); err != nil || n != 1 {
		t.Fatalf("send duration series: n=%d err=%v, want 1 (provider=resend)", n, err)
	}
	if got := histCount(t, reg, "contact_send_duration_seconds", "provider", "resend"); got != 2 {
		t.Fatalf("histogram sample count = %d, want 2", got)
	}
	if got := histSum(t, reg, "contact_send_duration_seconds"); got < 2.24 || got > 2.26 {
		t.Fatalf("histogram sum = %v, want ~2.25", got)
	}
}

func TestMetrics_CollectAndCount(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := observability.NewMetrics(reg)
	m.Submission("a", "f", "sent")
	m.Submission("b", "f", "sent")
	m.ProviderFailure("resend")
	m.ProviderFailure("smtp")
	m.AutoReplyFailure("a", "f")

	for name, want := range map[string]int{
		"contact_submissions_total":        2,
		"contact_provider_failures_total":  2,
		"contact_autoreply_failures_total": 1,
	} {
		got, err := testutil.GatherAndCount(reg, name)
		if err != nil || got != want {
			t.Errorf("%s: count=%d err=%v, want %d", name, got, err, want)
		}
	}
}

func TestMetrics_NilRegistererDoesNotPanic(t *testing.T) {
	m := observability.NewMetrics(nil)
	m.Submission("a", "f", "sent")
}

func TestMetrics_PreinitCreatesZeroSeries(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := observability.NewMetrics(reg)
	cfg := &config.Config{
		Providers: map[string]config.ProviderConfig{"resend": {Type: "resend"}, "smtp": {Type: "smtp"}},
		Sites: map[string]*config.Site{
			"a": {Name: "a", Forms: map[string]*config.Form{
				"contact": {Name: "contact", AutoReply: &config.AutoReply{Subject: "s", Body: "b"}},
				"quote":   {Name: "quote"},
			}},
			"b": {Name: "b", Forms: map[string]*config.Form{"contact": {Name: "contact"}}},
		},
	}
	m.Preinit(cfg)

	outcomes := []string{"sent", "spam", "invalid", "forbidden", "rate_limited", "send_failed"}
	for _, sf := range [][2]string{{"a", "contact"}, {"a", "quote"}, {"b", "contact"}} {
		for _, o := range outcomes {
			sel := `contact_submissions_total{form="` + sf[1] + `",outcome="` + o + `",site="` + sf[0] + `"}`
			if got := gatherValue(t, reg, sel); got != 0 {
				t.Errorf("%s = %v, want 0", sel, got)
			}
		}
	}
	gatherValue(t, reg, `contact_submissions_total{form="unknown",outcome="not_found",site="unknown"}`)
	gatherValue(t, reg, `contact_provider_failures_total{provider="resend"}`)
	gatherValue(t, reg, `contact_provider_failures_total{provider="smtp"}`)
	gatherValue(t, reg, `contact_autoreply_failures_total{form="contact",site="a"}`)

	for name, want := range map[string]int{
		"contact_submissions_total":        3*len(outcomes) + 1,
		"contact_provider_failures_total":  2,
		"contact_autoreply_failures_total": 1, // only forms with auto_reply
	} {
		if got, err := testutil.GatherAndCount(reg, name); err != nil || got != want {
			t.Errorf("%s: count=%d err=%v, want %d", name, got, err, want)
		}
	}

	// A later failure is then an increase from 0, which increase() sees.
	m.ProviderFailure("resend")
	if got := gatherValue(t, reg, `contact_provider_failures_total{provider="resend"}`); got != 1 {
		t.Fatalf("after failure = %v, want 1", got)
	}
}
