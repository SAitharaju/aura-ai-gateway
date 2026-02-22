package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestLatency tracks the latency of incoming completions requests.
	RequestLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "aura_ai_gateway_request_latency_seconds",
		Help:    "Latency of /v1/chat/completions requests.",
		Buckets: prometheus.DefBuckets,
	}, []string{"status"})

	// TotalTokens tracks total token usage per API key.
	TotalTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aura_ai_gateway_total_tokens",
		Help: "Total tokens consumed through the proxy.",
	}, []string{"api_key"})

	// ErrorRate tracks proxy errors by type.
	ErrorRate = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "aura_ai_gateway_errors_total",
		Help: "Total errors encountered by the proxy.",
	}, []string{"type"})
)
