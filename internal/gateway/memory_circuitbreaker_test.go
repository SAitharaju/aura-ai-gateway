package gateway_test

import (
	"aura-ai-gateway/internal/gateway"
	"testing"
)

func TestMemoryCircuitBreaker(t *testing.T) {
	cb := gateway.NewMemoryCircuitBreaker()
	apiKey := "test-key"

	// 1. Initial State Check
	allowed, err := cb.CheckLimit(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on CheckLimit: %v", err)
	}
	if !allowed {
		t.Errorf("expected new key to be allowed, got denied")
	}

	usage, err := cb.GetUsage(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on GetUsage: %v", err)
	}
	if usage != 0 {
		t.Errorf("expected usage 0, got %d", usage)
	}

	// 2. Add Usage
	err = cb.AddUsage(apiKey, 1000)
	if err != nil {
		t.Fatalf("unexpected error on AddUsage: %v", err)
	}

	usage, err = cb.GetUsage(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on GetUsage: %v", err)
	}
	expectedCost := int64(1000) * gateway.CostPerTokenMicroDollars
	if usage != expectedCost {
		t.Errorf("expected usage %d, got %d", expectedCost, usage)
	}

	// 3. Exceed Limit
	// Calculate tokens needed to exceed MaxUsageMicroDollars
	tokensToExceed := int(gateway.MaxUsageMicroDollars/gateway.CostPerTokenMicroDollars) + 1
	err = cb.AddUsage(apiKey, tokensToExceed)
	if err != nil {
		t.Fatalf("unexpected error on AddUsage: %v", err)
	}

	allowed, err = cb.CheckLimit(apiKey)
	if err != nil {
		t.Fatalf("unexpected error on CheckLimit: %v", err)
	}
	if allowed {
		t.Errorf("expected key to be denied after exceeding limit, got allowed")
	}
}
