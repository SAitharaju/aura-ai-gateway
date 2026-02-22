package gateway_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"aura-ai-gateway/internal/gateway"
)

// MockCircuitBreaker is a simple mock for testing the proxy handler
type MockCircuitBreaker struct {
	Allowed bool
	Err     error
	Usage   int64
}

func (m *MockCircuitBreaker) CheckLimit(apiKey string) (bool, error) {
	return m.Allowed, m.Err
}

func (m *MockCircuitBreaker) AddUsage(apiKey string, tokenCount int) error {
	m.Usage += int64(tokenCount) * gateway.CostPerTokenMicroDollars
	return nil
}

func (m *MockCircuitBreaker) GetUsage(apiKey string) (int64, error) {
	return m.Usage, nil
}

func TestProxyHandler_ServeHTTP(t *testing.T) {
	// Setup a mock upstream server
	upstreamServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify injected options
		bodyBytes, _ := io.ReadAll(r.Body)
		var payload map[string]interface{}
		json.Unmarshal(bodyBytes, &payload)

		streamOpts, ok := payload["stream_options"].(map[string]interface{})
		if !ok || streamOpts["include_usage"] != true {
			t.Errorf("expected stream_options.include_usage to be true")
		}

		if payload["stream"] != true {
			t.Errorf("expected stream to be true")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstreamServer.Close()

	upstreamURL, _ := url.Parse(upstreamServer.URL)
	cb := &MockCircuitBreaker{Allowed: true}
	usageChan := make(chan gateway.UsageRecord, 1)

	proxyHandler := gateway.NewProxyHandler(upstreamURL, cb, usageChan)

	// Create test request
	reqBody := []byte(`{"model": "gpt-3.5-turbo", "messages": [{"role": "user", "content": "Hello!"}]}`)
	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestProxyHandler_RateLimit(t *testing.T) {
	upstreamURL, _ := url.Parse("http://dummy.com")
	cb := &MockCircuitBreaker{Allowed: false} // Simulate rate limit hit
	usageChan := make(chan gateway.UsageRecord, 1)

	proxyHandler := gateway.NewProxyHandler(upstreamURL, cb, usageChan)

	req := httptest.NewRequest("POST", "/v1/chat/completions", bytes.NewReader([]byte("{}")))
	req.Header.Set("Authorization", "Bearer test-key")

	rr := httptest.NewRecorder()
	proxyHandler.ServeHTTP(rr, req)

	if rr.Code != http.StatusPaymentRequired {
		t.Errorf("expected status 402 Payment Required, got %d", rr.Code)
	}
}
