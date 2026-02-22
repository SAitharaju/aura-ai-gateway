package gateway

import (
	"sync"
	"sync/atomic"
)

// MemoryCircuitBreaker implements the CircuitBreaker interface using an in-memory sync.Map.
type MemoryCircuitBreaker struct {
	// usageMap stores apiKey (string) -> *int64 (pointer to micro-dollars atomic counter)
	usageMap syncMap
}

// syncMap is a custom generic wrapper around sync.Map for type safety
type syncMap struct {
	m sync.Map
}

func (s *syncMap) LoadOrStore(key string, value int64) *int64 {
	val, _ := s.m.LoadOrStore(key, &value)
	return val.(*int64)
}

func (s *syncMap) Load(key string) (*int64, bool) {
	val, ok := s.m.Load(key)
	if !ok {
		return nil, false
	}
	return val.(*int64), true
}

func NewMemoryCircuitBreaker() *MemoryCircuitBreaker {
	return &MemoryCircuitBreaker{}
}

// CheckLimit verifies if the given API key has exceeded the $10.00 limit.
// Checks are extremely fast in-memory map lookups.
func (r *MemoryCircuitBreaker) CheckLimit(apiKey string) (bool, error) {
	valRef, ok := r.usageMap.Load(apiKey)
	if !ok {
		// Key does not exist, usage is 0, allow request
		return true, nil
	}

	usage := atomic.LoadInt64(valRef)
	return usage < MaxUsageMicroDollars, nil
}

// AddUsage asynchronously increments the usage cost for the API key in memory.
func (r *MemoryCircuitBreaker) AddUsage(apiKey string, tokenCount int) error {
	cost := int64(tokenCount) * CostPerTokenMicroDollars

	// Ensure the key exists in the map
	valRef := r.usageMap.LoadOrStore(apiKey, 0)

	// Atomically add the cost to avoid race conditions from concurrent requests
	atomic.AddInt64(valRef, cost)

	return nil
}

// GetUsage retrieves the usage. If none is recorded, defaults to 0.
func (r *MemoryCircuitBreaker) GetUsage(apiKey string) (int64, error) {
	valRef, ok := r.usageMap.Load(apiKey)
	if !ok {
		return 0, nil
	}
	return atomic.LoadInt64(valRef), nil
}
