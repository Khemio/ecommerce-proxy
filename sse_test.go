package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func addTestSession(s *MarketplaceServer, id string) {
	s.saveSession(&Session{
		ID:        id,
		CreatedAt: time.Now(),
		Events:    make(chan SSEEvent, 1),
	})
}

func deliverTestWebhook(t *testing.T, s *MarketplaceServer, id string) {
	t.Helper()

	body := fmt.Appendf(nil, `{
        "id": 42,
        "name": "#1001",
        "order_number": 1001,
        "currency": "USD",
        "total_price": "15.00",
        "note_attributes": [
            {"name": "go_session_id", "value": %q}
        ],
        "line_items": [
            {
                "product_id": 100,
                "variant_id": 200,
                "title": "Test product",
                "quantity": 1,
                "price": "15.00"
            }
        ]
    }`, id)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/shopify/webhook",
		strings.NewReader(string(body)),
	)

	req.Header.Set(
		"X-Shopify-Hmac-Sha256",
		signWebhook(body, s.config.ShopifySecretToken),
	)

	resp := httptest.NewRecorder()
	s.HandleShopifyWebhook(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("webhook returned HTTP %d: %s",
			resp.Code, resp.Body.String())
	}
}

func requireOrderEvent(t *testing.T, stream io.Reader) {
	t.Helper()

	scanner := bufio.NewScanner(stream)
	eventName := ""

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "event: ") {
			eventName = strings.TrimPrefix(line, "event: ")
		}

		if strings.HasPrefix(line, "data: ") {
			if eventName != "order.created" {
				t.Fatalf("unexpected event: %q", eventName)
			}

			var order OrderEvent
			data := strings.TrimPrefix(line, "data: ")

			if err := json.Unmarshal([]byte(data), &order); err != nil {
				t.Fatalf("invalid SSE JSON: %v", err)
			}

			if order.OrderID != "42" {
				t.Fatalf("unexpected order ID: %q", order.OrderID)
			}

			return
		}
	}

	t.Fatalf("stream ended before order event: %v", scanner.Err())
}

func TestWebhookBeforeSSE(t *testing.T) {
	s := testMarketplaceServer()
	sessionID := "session-before-sse"

	addTestSession(s, sessionID)
	deliverTestWebhook(t, s, sessionID)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/events?sessionId="+sessionID,
		nil,
	)
	resp := httptest.NewRecorder()

	s.HandleSSE(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	requireOrderEvent(t, strings.NewReader(resp.Body.String()))

	if _, exists := s.getSession(sessionID); exists {
		t.Fatal("completed session should be removed")
	}
}

func TestWebhookWhileSSEConnected(t *testing.T) {
	s := testMarketplaceServer()
	sessionID := "session-connected"
	addTestSession(s, sessionID)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events", s.HandleSSE)

	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	ctx, cancel := context.WithTimeout(
		context.Background(), 5*time.Second,
	)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		httpServer.URL+"/api/v1/events?sessionId="+sessionID,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := httpServer.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE returned HTTP %d", resp.StatusCode)
	}

	// The SSE handler has opened and flushed the connection.
	deliverTestWebhook(t, s, sessionID)

	// Read the event from the live HTTP response stream.
	requireOrderEvent(t, resp.Body)

	if _, exists := s.getSession(sessionID); exists {
		t.Fatal("completed session should be removed")
	}
}

func TestSSEReconnectAfterDisconnect(t *testing.T) {
	s := testMarketplaceServer()
	sessionID := "session-reconnect"
	addTestSession(s, sessionID)

	handlerExited := make(chan struct{})

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/events",
		func(w http.ResponseWriter, r *http.Request) {
			defer close(handlerExited)
			s.HandleSSE(w, r)
		},
	)

	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		httpServer.URL+"/api/v1/events?sessionId="+sessionID,
		nil,
	)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	resp, err := httpServer.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	// Simulate Flutter closing the SSE connection.
	cancel()
	resp.Body.Close()

	select {
	case <-handlerExited:
		// Handler observed the disconnect.
	case <-time.After(5 * time.Second):
		t.Fatal("SSE handler did not exit after disconnect")
	}

	// The session must remain available.
	if _, exists := s.getSession(sessionID); !exists {
		t.Fatal("disconnect unexpectedly deleted session")
	}

	// The order arrives while Flutter is disconnected.
	deliverTestWebhook(t, s, sessionID)

	// Flutter reconnects.
	reconnect := httptest.NewRecorder()
	reconnectReq := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/events?sessionId="+sessionID,
		nil,
	)

	s.HandleSSE(reconnect, reconnectReq)

	requireOrderEvent(
		t,
		strings.NewReader(reconnect.Body.String()),
	)

	if _, exists := s.getSession(sessionID); exists {
		t.Fatal("session should be removed after event delivery")
	}
}

func TestDuplicateWebhookPreservesFirstOrder(t *testing.T) {
	s := testMarketplaceServer()
	sessionID := "duplicate-test"
	addTestSession(s, sessionID)

	// First delivery stores order 42.
	deliverTestWebhook(t, s, sessionID)

	// Second delivery claims a different order for the same session.
	body := []byte(fmt.Sprintf(`{
        "id": 43,
        "note_attributes": [
            {"name": "go_session_id", "value": %q}
        ]
    }`, sessionID))

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/shopify/webhook",
		bytes.NewReader(body),
	)
	req.Header.Set(
		"X-Shopify-Hmac-Sha256",
		signWebhook(body, s.config.ShopifySecretToken),
	)

	resp := httptest.NewRecorder()
	s.HandleShopifyWebhook(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}

	session, exists := s.getSession(sessionID)
	if !exists {
		t.Fatal("session disappeared")
	}

	session.mu.Lock()
	defer session.mu.Unlock()

	order, ok := session.CompletedOrder.Data.(OrderEvent)
	if !ok || order.OrderID != "42" {
		t.Fatal("duplicate webhook replaced the first order")
	}

	if len(session.Events) != 1 {
		t.Fatal("expected exactly one queued event")
	}
}

func TestSessionCleanup(t *testing.T) {
	s := testMarketplaceServer()

	s.saveSession(&Session{
		ID:        "expired",
		CreatedAt: time.Now().Add(-sessionTTL - time.Minute),
		Events:    make(chan SSEEvent, 1),
	})

	addTestSession(s, "active")

	s.cleanupSessions()

	if _, exists := s.getSession("expired"); exists {
		t.Fatal("expired session was not removed")
	}

	if _, exists := s.getSession("active"); !exists {
		t.Fatal("active session was incorrectly removed")
	}
}
