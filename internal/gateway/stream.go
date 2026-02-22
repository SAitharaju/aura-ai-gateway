package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
)

// UsageRecord represents the token usage structure sent to the background processor
type UsageRecord struct {
	APIKey     string
	TokenCount int
}

// StreamResponse streams SSE data from upstream to the client and extracts usage transparently.
func StreamResponse(w http.ResponseWriter, resp *http.Response, apiKey string, usageChan chan<- UsageRecord) {
	// 1. Copy Response Headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	// Ensure we can flush immediately to client
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Fallback for clients/middlewares that don't support SSE streaming
		return
	}

	// 2. Scan and stream the response line by line
	scanner := bufio.NewScanner(resp.Body)
	// We might receive large lines, expand scanner buffer if needed
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	var tokenCount int
	prefix := []byte("data: ")
	doneSequence := []byte("[DONE]")

	for scanner.Scan() {
		line := scanner.Bytes()

		// Write to client immediately
		w.Write(line)
		w.Write([]byte("\n"))
		flusher.Flush() // Crucial for sub-10ms latency per chunk

		// Look for Server-Sent Events starting with "data: "
		if bytes.HasPrefix(line, prefix) {
			data := bytes.TrimPrefix(line, prefix)
			// Ignore the final "[DONE]" message
			if bytes.HasPrefix(data, doneSequence) {
				continue
			}

			// Parse chunk payload
			// We optimize this by only looking for the `usage` object
			var chunk struct {
				Usage *struct {
					TotalTokens int `json:"total_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(data, &chunk); err == nil && chunk.Usage != nil {
				// Usage block detected
				tokenCount = chunk.Usage.TotalTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		// Non-blocking log. Ideally inject an observability logger here.
	}

	// 3. Dispatch usage record asynchronously
	// Push to background channel to avoid blocking the client disconnecting
	if tokenCount > 0 && apiKey != "" && usageChan != nil {
		select {
		case usageChan <- UsageRecord{APIKey: apiKey, TokenCount: tokenCount}:
			// Successfully pushed
		default:
			// Buffer full or channel blocked. In a production app, we should log a warning
			// or have a dead-letter queue so we don't drop billing data.
		}
	}
}
