package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
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

	// Generate a server-side ID.
	sessionID := uuid.NewString()
	fmt.Printf("Generated Session ID: %s\n", sessionID)

	// Create Shopify cart.
	// graphQLResp := CreateCartAndCheckout(
	// 	&s.config,
	// 	items,
	// 	sessionID,
	// )

	graphQLResp := CreateCartAndCheckout(&s.config, items)

	cart := graphQLResp.Data.CartCreate.Cart
	cartID := cart.Id
	cartToken := extractToken(cartID)

	session := &Session{
		ID: sessionID,
		// ClientID:  clientID,
		CartToken: cartToken,

		CreatedAt: time.Now(),

		// Small buffer protects us from the webhook
		// arriving before SSE starts.
		Events: make(chan OrderEvent, 1),
	}

	s.sessionsMu.Lock()

	// If this client already has a session, clean up the old one.
	// if old, exists := s.sessions[clientID]; exists {
	if old, exists := s.sessions[sessionID]; exists {
		close(old.Events)
	}

	s.sessions[sessionID] = session
	s.cartSessions[cartToken] = sessionID
	// s.sessions[clientID] = session
	// s.cartSessions[cartToken] = clientID

	s.sessionsMu.Unlock()

	// --- DELAYED SSE LOGIC ---
	// Simulating an event (e.g., Order Processed) sent 5 seconds later
	time.AfterFunc(5*time.Second, func() {
		s.sessionsMu.RLock()
		currentSession, exists := s.sessions[sessionID]
		s.sessionsMu.RUnlock()
		fmt.Println(exists)
		// fmt.Printf("SSE: %b\n", exists)
		// Verify the session hasn't been overwritten or closed in the meantime
		if exists && currentSession == session {
			event := OrderEvent{
				// OrderID: "TEST_ORDER_ID_1234567890",
				OrderID: sessionID,
			}
			// Safely attempt to send to the channel
			select {
			case currentSession.Events <- event:
				// Event successfully queued for SSE listener
			default:
				s.logger.Printf("Warning: Channel full, event dropped for client %s", sessionID)
			}
		}
	})
	// -------------------------

	response := CheckoutResponse{
		SessionId:   sessionID,
		Id:          cart.Id,
		CheckoutUrl: cart.CheckoutUrl,
		Quantity:    cart.Quantity,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleOrderSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")

	if sessionID == "" {
		http.Error(w, "missing clientId", http.StatusBadRequest)
		return
	}

	fmt.Printf("SSE Session ID: %s\n", sessionID)

	s.sessionsMu.RLock()
	session, exists := s.sessions[sessionID]
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
				s.logger.Printf("SSE session channel closed for session: %s", sessionID)
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
				sessionID,
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
	defer r.Body.Close()

	body, _ := io.ReadAll(r.Body)
	hmacHeader := r.Header.Get("X-Shopify-Hmac-Sha256")
	if !verifyHMAC(body, hmacHeader, s.config.ShopifySecretToken) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	var webhook ShopifyOrderWebhook

	if err := json.NewDecoder(r.Body).Decode(&webhook); err != nil {
		http.Error(w, "invalid webhook", http.StatusBadRequest)
		return
	}

	s.sessionsMu.RLock()

	// 'YourSessionID' would probably be CartId
	// session, exists := s.sessions[webhook.YourSessionID]
	sessionId, exists := s.cartSessions[webhook.CartID]
	session, exists := s.sessions[sessionId]

	s.sessionsMu.RUnlock()

	if !exists {
		// The order may have been created for an old/expired session.
		w.WriteHeader(http.StatusOK)
		return
	}

	event := OrderEvent{
		OrderID: strconv.FormatInt(webhook.ID, 10),
	}

	// Don't hold the mutex while potentially blocking.
	select {
	case session.Events <- event:
	default:
		s.logger.Printf(
			"event channel full for session %s",
			session.ID,
		)
	}

	w.WriteHeader(http.StatusOK)
}

func verifyHMAC(body []byte, hmacHeader, secret string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hmacHeader)) == 1
}
