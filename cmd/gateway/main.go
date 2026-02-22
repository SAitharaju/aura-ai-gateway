package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aura-ai-gateway/internal/metrics"
	"aura-ai-gateway/internal/observability"
	"aura-ai-gateway/internal/gateway"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

func main() {
	logger := observability.SetupLogger()
	logger.Info("Starting Aura AI Gateway")

	// Config Validation
	upstreamURLStr := os.Getenv("UPSTREAM_URL")
	if os.Getenv("MOCK_UPSTREAM") == "true" {
		logger.Info("Starting Mock Upstream Server on :8081")
		go startMockUpstreamServer()
		upstreamURLStr = "http://localhost:8081/v1/chat/completions"

		// Small delay to ensure the mock server starts
		time.Sleep(500 * time.Millisecond)
	} else if upstreamURLStr == "" {
		upstreamURLStr = "https://api.openai.com/v1/chat/completions"
	}

	upstreamURL, err := url.Parse(upstreamURLStr)
	if err != nil {
		logger.Error("Invalid UPSTREAM_URL", "error", err)
		os.Exit(1)
	}

	// 1. Initialize Circuit Breaker
	var cb gateway.CircuitBreaker
	if os.Getenv("USE_MEMORY_STORE") == "true" {
		logger.Info("Using In-Memory Circuit Breaker for local testing")
		cb = gateway.NewMemoryCircuitBreaker()
	} else {
		redisAddr := os.Getenv("REDIS_ADDR")
		if redisAddr == "" {
			redisAddr = "localhost:6379"
		}

		redisClient := redis.NewClient(&redis.Options{
			Addr: redisAddr,
		})
		if err := redisClient.Ping(context.Background()).Err(); err != nil {
			logger.Error("Failed to connect to Redis", "error", err)
			os.Exit(1)
		}
		cb = gateway.NewRedisCircuitBreaker(redisClient)
	}

	// 2. Start Background Usage Processor
	usageChan := make(chan gateway.UsageRecord, 1000)
	go func() {
		for record := range usageChan {
			if err := cb.AddUsage(record.APIKey, record.TokenCount); err != nil {
				logger.Error("Failed to add usage to Redis", "api_key", record.APIKey, "error", err)
				metrics.ErrorRate.WithLabelValues("redis_write").Inc()
			} else {
				metrics.TotalTokens.WithLabelValues(record.APIKey).Add(float64(record.TokenCount))
				logger.Info("Usage recorded", "api_key", record.APIKey, "tokens", record.TokenCount)
			}
		}
	}()

	// 3. Initialize Proxy Handler
	proxyHandler := gateway.NewProxyHandler(upstreamURL, cb, usageChan)

	// Define Routes
	http.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// In a fully robust version, we would wrap ResponseWriter to capture the exact status code.
		// For high-performance passthrough, assuming 200 or failure manually reported.
		proxyHandler.ServeHTTP(w, r)

		duration := time.Since(start).Seconds()
		metrics.RequestLatency.WithLabelValues("200").Observe(duration)
		logger.Info("Request processed", "method", r.Method, "path", r.URL.Path, "latency_sec", duration)
	})

	// Add an endpoint to check usage budget
	http.HandleFunc("/v1/usage", func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var apiKey string
		if len(authHeader) > 7 && authHeader[:7] == "Bearer " {
			apiKey = authHeader[7:]
		}

		if apiKey == "" {
			http.Error(w, "Unauthorized: provide API Key", http.StatusUnauthorized)
			return
		}

		usageMicro, err := cb.GetUsage(apiKey)
		if err != nil {
			logger.Error("Failed to get usage", "error", err)
			http.Error(w, "Failed to retrieve usage", http.StatusInternalServerError)
			return
		}

		usageDollars := float64(usageMicro) / 1000000.0
		limitDollars := float64(gateway.MaxUsageMicroDollars) / 1000000.0

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"api_key":           apiKey,
			"usage_dollars":     usageDollars,
			"limit_dollars":     limitDollars,
			"remaining_dollars": limitDollars - usageDollars,
		})
	})

	// Expose Prometheus Metrics endpoint
	http.Handle("/metrics", promhttp.Handler())

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr: ":" + port,
	}

	// 4. Start Server
	go func() {
		logger.Info("Listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	// 5. Graceful Shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
	}

	close(usageChan) // Allow usage processor to drain
	logger.Info("Server exiting")
}

// startMockUpstreamServer simulates a successful OpenAI streaming response for testing.
func startMockUpstreamServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Simulate streaming text
		chunks := []string{"Hello", "!", " This", " is", " a", " simulated", " streaming", " response."}
		for _, chunk := range chunks {
			data := fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"}}]}`, chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
			time.Sleep(100 * time.Millisecond)
		}

		// Send usage chunk
		usageData := `{"usage":{"prompt_tokens":10,"completion_tokens":8,"total_tokens":18}}`
		fmt.Fprintf(w, "data: %s\n\n", usageData)
		flusher.Flush()

		// Send [DONE] chunk
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	})

	if err := http.ListenAndServe(":8081", mux); err != nil {
		fmt.Printf("Mock upstream failed: %v\n", err)
	}
}
