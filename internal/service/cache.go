package service

import (
	"log/slog"
	"time"

	"github.com/samber/hot"
)

const (
	marketStateCacheTTL  = 30 * time.Second
	marketStateCacheSize = 500
)

// StateCache provides in-memory caching for market states with stale-while-revalidate.
type StateCache struct {
	cache *hot.HotCache[string, MarketState]
}

// NewStateCache creates a new market state cache.
// The loader function is called during background revalidation to refresh stale entries.
func NewStateCache(loader func(ids []string) (map[string]MarketState, error)) *StateCache {
	if loader == nil {
		panic("NewStateCache: loader must not be nil")
	}
	c := hot.NewHotCache[string, MarketState](hot.LRU, marketStateCacheSize).
		WithTTL(marketStateCacheTTL).
		WithRevalidation(marketStateCacheTTL, loader).
		WithRevalidationErrorPolicy(hot.KeepOnError).
		Build()
	return &StateCache{cache: c}
}

// Get returns a cached market state if available.
func (sc *StateCache) Get(id string) (MarketState, bool) {
	val, found, err := sc.cache.Get(id)
	if err != nil {
		slog.Warn("state cache error, treating as miss", "id", id, "error", err)
		return MarketState{}, false
	}
	if !found {
		return MarketState{}, false
	}
	return val, true
}

// Set stores a market state in the cache.
func (sc *StateCache) Set(id string, state MarketState) {
	sc.cache.Set(id, state)
}
