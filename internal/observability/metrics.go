package observability

import (
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics holds the service's Prometheus instruments. Label values are
// limited to site, form, provider and outcome; never form field values.
type Metrics struct {
	submissions     *prometheus.CounterVec
	sendDuration    *prometheus.HistogramVec
	providerFail    *prometheus.CounterVec
	autoReplyFailed *prometheus.CounterVec
}

// NewMetrics registers the instruments on reg.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	f := promauto.With(reg)
	return &Metrics{
		submissions: f.NewCounterVec(prometheus.CounterOpts{
			Name: "contact_submissions_total",
			Help: "Form submissions by site, form and outcome.",
		}, []string{"site", "form", "outcome"}),
		sendDuration: f.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "contact_send_duration_seconds",
			Help:    "Time spent delivering a message, by provider.",
			Buckets: prometheus.DefBuckets,
		}, []string{"provider"}),
		providerFail: f.NewCounterVec(prometheus.CounterOpts{
			Name: "contact_provider_failures_total",
			Help: "Failed delivery attempts by provider.",
		}, []string{"provider"}),
		autoReplyFailed: f.NewCounterVec(prometheus.CounterOpts{
			Name: "contact_autoreply_failures_total",
			Help: "Failed auto-replies by site and form.",
		}, []string{"site", "form"}),
	}
}

// SubmissionOutcomes are the outcome label values of a known site and form.
// They must match the submit package's outcomes (a test there checks it).
var SubmissionOutcomes = []string{"sent", "spam", "invalid", "forbidden", "rate_limited", "send_failed"}

// Labels used for a submission to an unknown site or form.
const (
	UnknownLabel    = "unknown"
	NotFoundOutcome = "not_found"
)

// Preinit creates every expected counter series at 0. Counter series are
// otherwise created lazily at 1, so increase() over the first failure after a
// start would be 0 and the alert would never fire.
func (m *Metrics) Preinit(cfg *config.Config) {
	for siteName, site := range cfg.Sites {
		for formName, form := range site.Forms {
			for _, o := range SubmissionOutcomes {
				m.submissions.WithLabelValues(siteName, formName, o).Add(0)
			}
			if form.AutoReply != nil {
				m.autoReplyFailed.WithLabelValues(siteName, formName).Add(0)
			}
		}
	}
	m.submissions.WithLabelValues(UnknownLabel, UnknownLabel, NotFoundOutcome).Add(0)
	for name := range cfg.Providers {
		m.providerFail.WithLabelValues(name).Add(0)
	}
}

// Submission counts one submission with its outcome.
func (m *Metrics) Submission(site, form, outcome string) {
	m.submissions.WithLabelValues(site, form, outcome).Inc()
}

// SendDuration records how long a provider took to deliver a message.
func (m *Metrics) SendDuration(provider string, d time.Duration) {
	m.sendDuration.WithLabelValues(provider).Observe(d.Seconds())
}

// ProviderFailure counts a failed delivery attempt.
func (m *Metrics) ProviderFailure(provider string) {
	m.providerFail.WithLabelValues(provider).Inc()
}

// AutoReplyFailure counts a failed auto-reply.
func (m *Metrics) AutoReplyFailure(site, form string) {
	m.autoReplyFailed.WithLabelValues(site, form).Inc()
}
