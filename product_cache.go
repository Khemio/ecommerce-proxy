package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const maxCachedProductResponse = 256 << 10 // 256 KiB

type productCacheEntry struct {
	body      []byte
	expiresAt time.Time
}

type ProductCache struct {
	mu         sync.Mutex
	entries    map[string]productCacheEntry
	flights    singleflight.Group
	ttl        time.Duration
	maxEntries int
}

func NewProductCache(ttl time.Duration, maxEntries int) *ProductCache {
	if ttl <= 0 || maxEntries <= 0 {
		panic("invalid product cache configuration")
	}

	return &ProductCache{
		entries:    make(map[string]productCacheEntry),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

func (c *ProductCache) lookup(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}

	if !time.Now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}

	return bytes.Clone(entry.body), true
}

func (c *ProductCache) store(key string, body []byte) {
	if len(body) > maxCachedProductResponse {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()

	// Remove expired entries.
	for k, entry := range c.entries {
		if !now.Before(entry.expiresAt) {
			delete(c.entries, k)
		}
	}

	// Evict the entry that expires soonest if full.
	if _, exists := c.entries[key]; !exists &&
		len(c.entries) >= c.maxEntries {

		var victim string

		for k, entry := range c.entries {
			if victim == "" ||
				entry.expiresAt.Before(c.entries[victim].expiresAt) {
				victim = k
			}
		}

		delete(c.entries, victim)
	}

	c.entries[key] = productCacheEntry{
		body:      bytes.Clone(body),
		expiresAt: now.Add(c.ttl),
	}
}

func (c *ProductCache) Load(
	ctx context.Context,
	key string,
	loader func(context.Context) ([]byte, error),
) ([]byte, error) {
	// Allows existing tests to use a server without a cache.
	if c == nil {
		return loader(ctx)
	}

	if body, ok := c.lookup(key); ok {
		return body, nil
	}

	// Only one upstream request per cache key runs at a time.
	resultCh := c.flights.DoChan(key, func() (any, error) {
		// Another request may have populated the cache.
		if body, ok := c.lookup(key); ok {
			return body, nil
		}

		// Bound shared work independently of the first caller's
		// cancellation, so other waiting clients can benefit.
		workCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx),
			10*time.Second,
		)
		defer cancel()

		body, err := loader(workCtx)
		if err != nil {
			return nil, err
		}

		c.store(key, body)
		return body, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()

	case result := <-resultCh:
		if result.Err != nil {
			return nil, result.Err
		}

		return bytes.Clone(result.Val.([]byte)), nil
	}
}

// Fetch the catalog through the cache while preserving the
// existing GraphQL response types and downstream transformations.
func (s *MarketplaceServer) fetchProductsCached(
	ctx context.Context,
	limit int,
) (GraphQLResponse[QueryResult], error) {
	var response GraphQLResponse[QueryResult]

	key := "catalog:" + strconv.Itoa(limit)

	body, err := s.productCache.Load(
		ctx,
		key,
		func(ctx context.Context) ([]byte, error) {
			result, err := FetchProducts(ctx, &s.config, limit)
			if err != nil {
				return nil, err
			}

			// Distinguish valid empty arrays from missing data.
			if result.Data.Products.Edges == nil {
				return nil, fmt.Errorf("Shopify products.edges is missing")
			}

			return json.Marshal(result)
		},
	)
	if err != nil {
		return response, err
	}

	err = json.Unmarshal(body, &response)
	return response, err
}
