package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCatalogCacheHit(t *testing.T) {
	var calls atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			calls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(
				`{"data":{"products":{"edges":[]}}}`,
			))
		},
	))
	defer upstream.Close()

	s := testMarketplaceServer()
	s.config.StorefrontEndpoint = upstream.URL
	s.productCache = NewProductCache(time.Minute, 128)

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(
			http.MethodGet, "/api/v1/products?limit=10", nil,
		)
		resp := httptest.NewRecorder()

		s.HandleFetchVendorProducts(resp, req)

		if resp.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.Code)
		}
		if strings.TrimSpace(resp.Body.String()) != "[]" {
			t.Fatalf("unexpected body: %s", resp.Body.String())
		}
	}

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 Shopify request, got %d", got)
	}
}

func TestCatalogDoesNotCacheUpstreamFailure(t *testing.T) {
	var calls atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(
				`{"data":{"products":{"edges":[]}}}`,
			))
		},
	))
	defer upstream.Close()

	s := testMarketplaceServer()
	s.config.StorefrontEndpoint = upstream.URL
	s.productCache = NewProductCache(time.Minute, 128)

	expected := []int{
		http.StatusBadGateway,
		http.StatusOK,
	}

	for _, want := range expected {
		req := httptest.NewRequest(
			http.MethodGet, "/api/v1/products", nil,
		)
		resp := httptest.NewRecorder()

		s.HandleFetchVendorProducts(resp, req)

		if resp.Code != want {
			t.Fatalf("expected %d, got %d", want, resp.Code)
		}
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 Shopify attempts, got %d", got)
	}
}

func TestCatalogCacheExpiration(t *testing.T) {
	cache := NewProductCache(time.Minute, 2)

	var calls atomic.Int32

	loader := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			return []byte("first"), nil
		}
		return []byte("second"), nil
	}

	ctx := context.Background()

	first, err := cache.Load(ctx, "catalog:10", loader)
	if err != nil || string(first) != "first" {
		t.Fatalf("first load: %q, %v", first, err)
	}

	// Simulate the passage of time.
	cache.mu.Lock()
	entry := cache.entries["catalog:10"]
	entry.expiresAt = time.Now().Add(-time.Second)
	cache.entries["catalog:10"] = entry
	cache.mu.Unlock()

	second, err := cache.Load(ctx, "catalog:10", loader)
	if err != nil || string(second) != "second" {
		t.Fatalf("second load: %q, %v", second, err)
	}

	if got := calls.Load(); got != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", got)
	}
}

func TestProductCacheEviction(t *testing.T) {
	cache := NewProductCache(time.Minute, 2)

	cache.store("first", []byte("1"))
	cache.store("second", []byte("2"))

	// Set distinct expiration times to make eviction deterministic.
	cache.mu.Lock()

	first := cache.entries["first"]
	first.expiresAt = time.Now().Add(10 * time.Second)
	cache.entries["first"] = first

	second := cache.entries["second"]
	second.expiresAt = time.Now().Add(30 * time.Second)
	cache.entries["second"] = second

	cache.mu.Unlock()

	cache.store("third", []byte("3"))

	if _, ok := cache.lookup("first"); ok {
		t.Fatal("oldest-expiring entry was not evicted")
	}

	if _, ok := cache.lookup("second"); !ok {
		t.Fatal("second entry should remain")
	}

	if _, ok := cache.lookup("third"); !ok {
		t.Fatal("new entry was not stored")
	}

	if len(cache.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(cache.entries))
	}
}

func TestCatalogConcurrentCacheMiss(t *testing.T) {
	cache := NewProductCache(time.Minute, 4)

	var calls atomic.Int32

	started := make(chan struct{})
	release := make(chan struct{})
	startWorkers := make(chan struct{})

	var releaseOnce sync.Once
	unblock := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer unblock()

	loader := func(ctx context.Context) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}

		<-release
		return []byte("catalog"), nil
	}

	const workers = 20

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-startWorkers

			body, err := cache.Load(
				context.Background(),
				"catalog:10",
				loader,
			)
			if err != nil {
				t.Errorf("cache load failed: %v", err)
				return
			}
			if string(body) != "catalog" {
				t.Errorf("unexpected response: %q", body)
			}
		}()
	}

	// Start all 20 requests together.
	close(startWorkers)

	select {
	case <-started:
		// Upstream request is in progress.
	case <-time.After(3 * time.Second):
		unblock()
		wg.Wait()
		t.Fatal("upstream loader never started")
	}

	unblock()
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}
}
