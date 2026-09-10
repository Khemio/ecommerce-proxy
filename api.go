package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func extractToken(cartID string) string {
	parts := strings.Split(cartID, "?")
	segments := strings.Split(parts[0], "/")
	return segments[len(segments)-1]
}

func (s *MarketplaceServer) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"operational"}`))
}

func (s *MarketplaceServer) HandleFetchVendorProduct(w http.ResponseWriter, r *http.Request) {
	// Extract path parameter using the standard library method
	productId := r.PathValue("product_id")
	if productId == "" {
		http.Error(w, "Product ID required", http.StatusBadRequest)
		return
	}

	var graphQLResp GraphQLResponse[ProductQueryResult] = FetchProduct(&s.config, productId)
	product := TransformGraphQLResponse(graphQLResp.Data.Product).(map[string]any)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(product); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleFetchVendorProducts(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if i, err := strconv.Atoi(s); err == nil {
			limit = i
		}
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	var graphQLResp GraphQLResponse[QueryResult] = FetchProducts(&s.config, limit)

	for i := range graphQLResp.Data.Products.Edges {
		edge := &graphQLResp.Data.Products.Edges[i]
		*edge = TransformGraphQLResponse(*edge).(map[string]any)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(TransformGraphQLResponse(graphQLResp.Data.Products.Edges)); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	var items []Item

	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	clientID := r.Header.Get("X-Client-Id")
	if clientID == "" {
		http.Error(w, "missing X-Client-Id", http.StatusBadRequest)
		return
	}

	fmt.Printf("Checkout: %s\n", clientID)

	graphQLResp := CreateCartAndCheckout(&s.config, items)

	cart := graphQLResp.Data.CartCreate.Cart
	cartID := cart.Id
	cartToken := extractToken(cartID)

	session := &Session{
		ClientID:  clientID,
		CartToken: cartToken,
		CreatedAt: time.Now(),

		Events: make(chan OrderEvent, 1),
	}

	s.sessionsMu.Lock()

	// If this client already has a session, clean up the old one.
	if old, exists := s.sessions[clientID]; exists {
		close(old.Events)
	}

	s.sessions[clientID] = session

	s.sessionsMu.Unlock()

	// --- DELAYED SSE LOGIC ---
	// Simulating an event (e.g., Order Processed) sent 5 seconds later
	time.AfterFunc(5*time.Second, func() {
		s.sessionsMu.RLock()
		currentSession, exists := s.sessions[clientID]
		s.sessionsMu.RUnlock()

		fmt.Println(exists)
		// fmt.Printf("SSE: %b\n", exists)

		// Verify the session hasn't been overwritten or closed in the meantime
		if exists && currentSession == session {
			event := OrderEvent{
				// OrderID: "TEST_ORDER_ID_1234567890",
				OrderID: clientID,
			}

			// Safely attempt to send to the channel
			select {
			case currentSession.Events <- event:
				// Event successfully queued for SSE listener
			default:
				s.logger.Printf("Warning: Channel full, event dropped for client %s", clientID)
			}
		}
	})
	// -------------------------

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := TransformGraphQLResponse(cart)

	// if err := json.NewEncoder(w).Encode(TransformGraphQLResponse(graphQLResp.Data.CartCreate.Cart)); err != nil {
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleOrderSSE(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("clientId")

	if clientID == "" {
		http.Error(w, "missing clientId", http.StatusBadRequest)
		return
	}

	fmt.Printf("SSE: %s\n", clientID)

	s.sessionsMu.RLock()
	session, exists := s.sessions[clientID]
	s.sessionsMu.RUnlock()

	if !exists {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	// Required for SSE.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(
			w,
			"streaming unsupported",
			http.StatusInternalServerError,
		)
		return
	}

	// Tell client that the SSE connection is alive.
	fmt.Fprint(w, ": connected\n\n")
	// fmt.Fprint(w, "event: connected\ndata: ack\n\n")
	flusher.Flush()

	ctx := r.Context()

	heartbeatTicker := time.NewTicker(30 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {

		case event, ok := <-session.Events:

			if !ok {
				s.logger.Printf("SSE session channel closed for client: %s", clientID)
				return
			}

			data, err := json.Marshal(event)
			if err != nil {
				s.logger.Printf("failed encoding SSE event: %v", err)
				continue
			}

			fmt.Fprintf(w, "event: order_completed\n")
			fmt.Fprintf(w, "data: %s\n\n", data)

			flusher.Flush()

			// Order has been delivered.
			return

		case <-ctx.Done():
			s.logger.Printf(
				"SSE client disconnected: %s",
				clientID,
			)
			return

		// case <-time.After(30 * time.Second):
		case <-heartbeatTicker.C:
			// Keep connection alive.
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func (s *MarketplaceServer) HandleShopifyWebhook(w http.ResponseWriter, r *http.Request) {
	var items []Item

	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	graphQLResp := CreateCartAndCheckout(&s.config, items)

	w.Header().Set("Content-Type", "application/json")
	// w.Write(body)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(TransformGraphQLResponse(graphQLResp.Data.CartCreate.Cart)); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}
