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

	sessionID := uuid.NewString()

	graphQLResp := CreateCartAndCheckout(
		&s.config,
		items,
		sessionID,
	)

	cart := graphQLResp.Data.CartCreate.Cart

	session := &Session{
		ID: sessionID,
		// CartToken: cartToken,

		CreatedAt: time.Now(),

		// Small buffer protects us from the webhook
		// arriving before SSE starts.
		Events: make(chan OrderEvent, 1),
	}

	// s.sessionsMu.Lock()

	// // If this client already has a session, clean up the old one.
	// if old, exists := s.sessions[sessionID]; exists {
	// 	close(old.Events)
	// }

	// s.sessions[sessionID] = session

	// s.sessionsMu.Unlock()

	s.saveSession(session)

	response := CheckoutResponse{
		SessionId:   sessionID,
		Id:          cart.Id,
		CheckoutUrl: cart.CheckoutUrl,
		Quantity:    cart.Quantity,
		Attributes:  cart.Attributes, //? Probably not necessary
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
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	fmt.Printf("SSE Session ID: %s\n", sessionID)

	// s.sessionsMu.RLock()
	// session, exists := s.sessions[sessionID]
	// s.sessionsMu.RUnlock()
	session, exists := s.getSession(sessionID)

	if !exists {
		msg := fmt.Sprintf("Server Error: Session %s not found\n", sessionID)
		http.Error(w, msg, http.StatusNotFound)
		return
	}

	// Required for SSE.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, ": connected\n\n")
	// fmt.Fprint(w, "event: connected\ndata: ack\n\n")
	flusher.Flush()

	session.mu.Lock()

	completedOrder := session.CompletedOrder

	session.mu.Unlock()

	if completedOrder != nil {
		if err := writeSSEOrder(w, flusher, *completedOrder); err != nil {
			s.logger.Printf("failed writing stored SSE event: %v", err)
		}

		s.logger.Printf("Finished writing Completed Order to session: %s\n", sessionID)
		s.deleteSession(sessionID)

		return
	}

	ctx := r.Context()

	heartbeatTicker := time.NewTicker(15 * time.Second)
	defer heartbeatTicker.Stop()

	for {
		select {

		case event, ok := <-session.Events:

			if !ok {
				s.logger.Printf("SSE session channel closed for session: %s", sessionID)
				return
			}

			if err := writeSSEOrder(w, flusher, event); err != nil {
				s.logger.Printf("Failed writing SSE event: %v", err)
				return
			}

			s.logger.Printf("Order sent, disposing of session: %s", sessionID)
			s.deleteSession(sessionID)

			return

		// case <-time.After(30 * time.Second):
		case <-heartbeatTicker.C:
			// Keep connection alive.
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()

		case <-ctx.Done():
			s.logger.Printf(
				"SSE client disconnected: %s",
				sessionID,
			)
			return
		}
	}
}

func (s *MarketplaceServer) HandleShopifyWebhook(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)

	if err != nil {
		http.Error(
			w,
			"failed reading webhook",
			http.StatusBadRequest,
		)
		return
	}

	hmacHeader := r.Header.Get("X-Shopify-Hmac-Sha256")
	if !verifyHMAC(body, hmacHeader, s.config.ShopifySecretToken) {
		s.logger.Println("Failed HMAC verification")
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusOK)

	var webhook ShopifyOrderWebhook

	if err := json.Unmarshal(body, &webhook); err != nil {
		s.logger.Println("Failed to parse response: ", err)
		return
	}

	// fmt.Printf("Webhook data struct: \n%+v \n", webhook)
	// fmt.Println("Webhook OrderID: ", webhook.ID)
	// fmt.Println("Webhook SessionID: ", webhook.Attributes[0].Value)]

	sessionID := ""

	for _, attribute := range webhook.Attributes {
		if attribute.Key == "go_session_id" {
			sessionID = attribute.Value
			break
		}
	}

	if sessionID == "" {
		s.logger.Printf(
			"Shopify order %d has no go_session_id",
			webhook.ID,
		)

		return
	}

	session, exists := s.getSession(sessionID)

	if !exists {
		// Session may have expired or already been handled.
		s.logger.Printf("session not found: %s", sessionID)
		return
	}

	event := OrderEvent{
		OrderID: fmt.Sprintf("%d", webhook.ID),
	}

	session.mu.Lock()

	session.CompletedOrder = &event

	session.mu.Unlock()
	s.logger.Printf("order event created")

	// Don't hold the mutex while potentially blocking.
	select {
	case session.Events <- event:
		s.logger.Printf("sent order %d to session %s", webhook.ID, sessionID)
	default:
		s.logger.Printf(
			"session %s event channel already full",
			sessionID,
		)
	}
}

func verifyHMAC(body []byte, hmacHeader string, secret string) bool {
	if hmacHeader == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return subtle.ConstantTimeCompare([]byte(expected), []byte(hmacHeader)) == 1
}

func writeSSEOrder(w http.ResponseWriter, flusher http.Flusher, event OrderEvent) error {
	data, err := json.Marshal(event)

	if err != nil {
		return err
	}

	fmt.Fprint(w, "event: order_completed\n")

	fmt.Fprintf(w, "data: %s\n\n", data)

	flusher.Flush()

	return nil
}

func extractToken(cartID string) string {
	if cartID == "" {
		return ""
	}

	parts := strings.SplitN(cartID, "?", 2)

	segments := strings.Split(parts[0], "/")

	if len(segments) == 0 {
		return ""
	}

	return segments[len(segments)-1]
}
