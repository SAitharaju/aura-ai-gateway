package gateway_test

import (
	"context"
	"testing"
	"time"

	"aura-ai-gateway/internal/gateway"
	"github.com/redis/go-redis/v9"
)

// TestRedisCircuitBreaker requires a running Redis/Valkey instance on localhost:6379 to pass.
// This acts as an integration test.
func TestRedisCircuitBreaker(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx := context.Background()

	// Short timeout ping to check if Redis is alive
	pingCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Skip("Skipping Redis integration test because Redis is not active at localhost:6379")
	}

	cb := gateway.NewRedisCircuitBreaker(client)
	apiKey := "test-redis-key"

	// Cleanup before and after test
	client.Del(ctx, "apikey:"+apiKey+":usage")
	defer client.Del(ctx, "apikey:"+apiKey+":usage")

	// 1. Initial State Check
	allowed, err := cb.CheckLimit(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on CheckLimit: %v", err)
	}
	if !allowed {
		t.Errorf("expected new key to be allowed, got denied")
	}

	// 2. Add Usage
	err = cb.AddUsage(apiKey, 500)
	if err != nil {
		t.Fatalf("unexpected error on AddUsage: %v", err)
	}

	usage, err := cb.GetUsage(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on GetUsage: %v", err)
	}
	expectedCost := int64(500) * gateway.CostPerTokenMicroDollars
	if usage != expectedCost {
		t.Errorf("expected usage %d, got %d", expectedCost, usage)
	}
}
