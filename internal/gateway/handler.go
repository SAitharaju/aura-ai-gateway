package gateway

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// CircuitBreaker defines the interface for the Redis-backed circuit breaker.
// We declare it here so the proxy package is decoupled and easily testable via mocks.
type CircuitBreaker interface {
	CheckLimit(apiKey string) (bool, error)
	AddUsage(apiKey string, tokenCount int) error
	GetUsage(apiKey string) (int64, error)
}

// ProxyHandler is responsible for intercepting and forwarding OpenAI-compatible requests.
type ProxyHandler struct {
	upstreamURL    *url.URL
	circuitBreaker CircuitBreaker
	usageChan      chan<- UsageRecord // Buffered channel for asynchronous billing
}

// NewProxyHandler initializes a new HTTP handler for the proxy.
func NewProxyHandler(upstream *url.URL, cb CircuitBreaker, usageChan chan<- UsageRecord) *ProxyHandler {
	return &ProxyHandler{
		upstreamURL:    upstream,
		circuitBreaker: cb,
		usageChan:      usageChan,
	}
}

func (h *ProxyHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 1. Extract API Key from Authorization header
	authHeader := r.Header.Get("Authorization")
	var apiKey string
	if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
		apiKey = authHeader[7:]
	}

	// 2. Check Circuit Breaker (Block request if over $10.00 limit)
	if apiKey != "" && h.circuitBreaker != nil {
		allowed, err := h.circuitBreaker.CheckLimit(apiKey)
		if err != nil {
			http.Error(w, "Error validating rate limit", http.StatusInternalServerError)
			return
		}
		if !allowed {
			http.Error(w, "Limit Exceeded: Usage > $10.00", http.StatusPaymentRequired)
			return
		}
	}

	// 3. Read incoming request body to inject `stream_options`
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var payload map[string]interface{}
	if len(bodyBytes) > 0 {
		// Use a json Decoder with UseNumber so we don't convert int to float64 silently
		decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
			return
		}
	}

	if payload == nil {
		payload = make(map[string]interface{})
	}

	// Inject stream_options: {"include_usage": true} so the upstream sends back token usage
	// Also ensure "stream": true is set for this workflow
	payload["stream"] = true
	payload["stream_options"] = map[string]interface{}{
		"include_usage": true,
	}

	modifiedBody, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "Error marshaling modified payload", http.StatusInternalServerError)
		return
	}

	// 4. Construct Upstream Request
	upstreamReq, err := http.NewRequest(r.Method, h.upstreamURL.String(), bytes.NewReader(modifiedBody))
	if err != nil {
		http.Error(w, "Error creating upstream request", http.StatusInternalServerError)
		return
	}

	// Copy headers, avoiding Content-Length since body length has changed
	for k, vv := range r.Header {
		if k == "Content-Length" {
			continue
		}
		for _, v := range vv {
			upstreamReq.Header.Add(k, v)
		}
	}
	upstreamReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(modifiedBody)))

	// 5. Send to Upstream
	client := &http.Client{}
	resp, err := client.Do(upstreamReq)
	if err != nil {
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 6. Pass response to stream handler
	StreamResponse(w, resp, apiKey, h.usageChan)
}
