package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSSEConnectionLimit(t *testing.T) {
	limiter := NewSSEConnectionLimiter(1)

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	handler := limiter.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hold" {
				close(started)
				<-release
			}
			w.WriteHeader(http.StatusOK)
		},
	))

	// Simulate a long-lived SSE connection.
	go func() {
		defer close(finished)

		resp := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hold", nil)
		handler.ServeHTTP(resp, req)
	}()

	// Wait until the first request occupies the slot.
	<-started

	// Another connection should be rejected.
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(
		blocked,
		httptest.NewRequest(http.MethodGet, "/events", nil),
	)

	if blocked.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", blocked.Code)
	}

	// Disconnect the first client.
	close(release)
	<-finished

	// The connection slot should now be reusable.
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(
		accepted,
		httptest.NewRequest(http.MethodGet, "/events", nil),
	)

	if accepted.Code != http.StatusOK {
		t.Errorf("expected 200 after release, got %d", accepted.Code)
	}
}

func TestSSEPerIPLimit(t *testing.T) {
	limiter := NewSSEPerIPLimiter(1, false)

	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})

	handler := limiter.Middleware(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/hold" {
				close(started)
				<-release
			}
			w.WriteHeader(http.StatusOK)
		},
	))

	go func() {
		defer close(finished)

		req := httptest.NewRequest(http.MethodGet, "/hold", nil)
		req.RemoteAddr = "192.0.2.1:1000"

		handler.ServeHTTP(httptest.NewRecorder(), req)
	}()

	<-started

	// Same IP, different port: rejected.
	sameIP := httptest.NewRequest(http.MethodGet, "/", nil)
	sameIP.RemoteAddr = "192.0.2.1:2000"

	rejected := httptest.NewRecorder()
	handler.ServeHTTP(rejected, sameIP)

	if rejected.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rejected.Code)
	}

	// A different IP can connect.
	otherIP := httptest.NewRequest(http.MethodGet, "/", nil)
	otherIP.RemoteAddr = "192.0.2.2:3000"

	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, otherIP)

	if accepted.Code != http.StatusOK {
		t.Errorf("expected 200 for other IP, got %d", accepted.Code)
	}

	// Disconnect the first client.
	close(release)
	<-finished

	// Its IP can connect again.
	retried := httptest.NewRecorder()
	handler.ServeHTTP(retried, sameIP)

	if retried.Code != http.StatusOK {
		t.Errorf("expected 200 after release, got %d", retried.Code)
	}
}
