// Package metrics holds the Prometheus instruments the contact form exposes.
//
// Why these exist: /metrics used to report only promhttp's own request counters,
// so a form that accepted a submission and then failed to hand it to the mail
// relay looked exactly like a healthy one from the outside. The submission
// counters are what let a scrape rule tell the two apart — and the "last
// success" timestamp is what catches a form that is silently dead while nothing
// is even trying to submit.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const namespace = "contact_form"

var (
	// Submissions counts finished handler runs, one per request, by outcome:
	// ok | invalid | error. The three sum to the number of POSTs received.
	Submissions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "submissions_total",
		Help:      "Contact form submissions by website and result (ok, invalid, error).",
	}, []string{"website", "result"})

	// Errors splits failures by the stage that failed, so a broken validator
	// (bind) is distinguishable from a broken mail path (send).
	Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "errors_total",
		Help:      "Contact form failures by website and stage (bind, send).",
	}, []string{"website", "stage"})

	// LastSuccessTimestamp is the wall-clock time of the last successful
	// submission. A rule on "no success for N days" is how a dead form is caught
	// without sending test mail through the real relay.
	LastSuccessTimestamp = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_success_timestamp_seconds",
		Help:      "Unix time of the last successful contact form submission per website.",
	}, []string{"website"})

	// SendDuration times the SMTP hop, the part that can hang.
	SendDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "send_duration_seconds",
		Help:      "Time spent handing a submission to the mail relay.",
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	}, []string{"website"})
)
