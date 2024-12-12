package monitoring

import (
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	Devices         prometheus.Gauge
	Info            *prometheus.GaugeVec
	RequestTotal    *prometheus.CounterVec
	Duration        *prometheus.HistogramVec
	DurationSummary prometheus.Summary
}

func PrometheusNewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Info: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "myapp",
			Name:      "info",
			Help:      "Information about app env",
		},
			[]string{"version"}),
		RequestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "myapp",
			Name:      "number_total_request",
			Help:      "Number of total request",
		}, []string{"response_code", "method"}),
		Duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: "myapp",
			Name:      "request_duration_seconds",
			// 4 times larger for apdes score
			// Buckets: prometheus.ExponentialBuckets(0.1, 1.5, 5),
			// Buckets: prometheus.LinearBuckets(0.1, 0.05, 5),
			Buckets: []float64{.1, .15, .2, .25, .3},
			Help:    "Request duration in seconds",
		}, []string{"status", "method", "route"}),
		DurationSummary: prometheus.NewSummary(prometheus.SummaryOpts{
			Namespace:  "myapp",
			Name:       "request_duration_summary_seconds",
			Help:       "Request duration in seconds",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
	}

	reg.MustRegister(m.Info, m.RequestTotal, m.Duration, m.DurationSummary)
	return m
}
