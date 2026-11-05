package observability

import (
	"time"

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
