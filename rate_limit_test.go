package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiter(t *testing.T) {
	// Two immediate requests allowed; third rejected.
	limiter := NewIPRateLimiter(time.Hour, 2, 100)

	handler := limiter.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		},
	))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "192.0.2.1:1234"

		resp := httptest.NewRecorder()
		handler.ServeHTTP(resp, req)

		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
			if resp.Header().Get("Retry-After") == "" {
				t.Fatal("missing Retry-After header")
			}
		}

		if resp.Code != want {
			t.Fatalf("request %d: got %d, want %d",
				i+1, resp.Code, want)
		}
	}

	// Another IP has an independent token bucket.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.2:5678"

	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("other client got %d", resp.Code)
	}
}

func TestResolveClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("Fly-Client-IP", "198.51.100.20")

	directIP, err := resolveClientIP(req, false)
	if err != nil || directIP != "192.0.2.10" {
		t.Fatalf("direct IP: %q, error: %v", directIP, err)
	}

	flyIP, err := resolveClientIP(req, true)
	if err != nil || flyIP != "198.51.100.20" {
		t.Fatalf("Fly IP: %q, error: %v", flyIP, err)
	}

	req.Header.Del("Fly-Client-IP")

	if _, err := resolveClientIP(req, true); err == nil {
		t.Fatal("expected error for missing trusted proxy header")
	}
}
