package gateway

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const (
	// Limit is $10.00, represented in micro-dollars
	// $10.00 * 1,000,000 = 10,000,000 micro-dollars
	MaxUsageMicroDollars = 10000000

	// Assuming a flat rate heuristic for calculation: $0.002 per 1000 tokens (gpt-3.5-turbo equivalent)
	// 1 token = 0.000002 dollars = 2 micro-dollars
	CostPerTokenMicroDollars = 2
)

// RedisCircuitBreaker implements the CircuitBreaker interface using Redis.
type RedisCircuitBreaker struct {
	client *redis.Client
}

func NewRedisCircuitBreaker(client *redis.Client) *RedisCircuitBreaker {
	return &RedisCircuitBreaker{
		client: client,
	}
}

func (r *RedisCircuitBreaker) getUsageKey(apiKey string) string {
	return fmt.Sprintf("apikey:%s:usage", apiKey)
}

// CheckLimit verifies if the given API key has exceeded the $10.00 limit.
// Checks are extremely fast O(1) string lookups in Redis.
func (r *RedisCircuitBreaker) CheckLimit(apiKey string) (bool, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.getUsageKey(apiKey)).Result()
	if err == redis.Nil {
		// Key does not exist, usage is 0, allow request
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("redis get error: %w", err)
	}

	usage, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return false, fmt.Errorf("invalid usage value in redis: %w", err)
	}

	return usage < MaxUsageMicroDollars, nil
}

// AddUsage asynchronously increments the usage cost for the API key.
func (r *RedisCircuitBreaker) AddUsage(apiKey string, tokenCount int) error {
	cost := int64(tokenCount) * CostPerTokenMicroDollars
	ctx := context.Background()
	return r.client.IncrBy(ctx, r.getUsageKey(apiKey), cost).Err()
}

// GetUsage retrieves the total usage cost tracked for an API key.
func (r *RedisCircuitBreaker) GetUsage(apiKey string) (int64, error) {
	ctx := context.Background()
	val, err := r.client.Get(ctx, r.getUsageKey(apiKey)).Result()
	if err == redis.Nil {
		// Key does not exist, usage is 0
		return 0, nil
	} else if err != nil {
		return 0, fmt.Errorf("redis get error: %w", err)
	}

	usage, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid usage value in redis: %w", err)
	}

	return usage, nil
}
