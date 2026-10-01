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
		Events: make(chan SSEEvent, 1),
	}

	s.saveSession(session)

	s.testSSEDelayed(sessionID)

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

func (s *MarketplaceServer) HandleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")

	if sessionID == "" {
		http.Error(w, "missing sessionId", http.StatusBadRequest)
		return
	}

	fmt.Printf("SSE Session ID: %s\n", sessionID)

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
	flusher.Flush()

	session.mu.Lock()

	completedOrder := session.CompletedOrder

	session.mu.Unlock()

	if completedOrder != nil {
		if err := writeSSEEvent(w, flusher, *completedOrder); err != nil {
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

			if err := writeSSEEvent(w, flusher, event); err != nil {
				s.logger.Printf("Failed writing SSE event: %v", err)
				return
			}

			s.logger.Printf("Order sent, disposing of session: %s", sessionID)
			s.deleteSession(sessionID)

			return

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

	var order ShopifyOrderWebhook

	if err := json.Unmarshal(body, &order); err != nil {
		s.logger.Println("Failed to parse response: ", err)
		return
	}

	sessionID := order.SessionID()

	if sessionID == "" {
		s.logger.Printf(
			"Shopify order %d has no go_session_id",
			order.ID,
		)

		return
	}

	session, exists := s.getSession(sessionID)

	if !exists {
		// Session may have expired or already been handled.
		s.logger.Printf("session not found: %s", sessionID)
		return
	}

	orderItems := make([]OrderItem, len(order.LineItems))

	for i, item := range order.LineItems {
		orderItems[i] = OrderItem{
			ProductID: strconv.FormatInt(item.ProductID, 10),
			VariantID: strconv.FormatInt(item.VariantID, 10),
			Title:     item.Title,
			Quantity:  item.Quantity,
			Price:     item.Price,
		}
	}

	event := SSEEvent{
		Event: "order.created",
		Data: OrderEvent{
			OrderID:            strconv.FormatInt(order.ID, 10),
			Name:               order.Name,
			ConfirmationNumber: order.ConfirmationNumber,
			OrderNumber:        order.OrderNumber,
			Currency:           order.Currency,
			TotalPrice:         order.TotalPrice,
			LineItems:          orderItems,
		},
	}

	session.mu.Lock()

	session.CompletedOrder = &event

	session.mu.Unlock()
	s.logger.Printf("order event created")

	// Don't hold the mutex while potentially blocking.
	select {
	case session.Events <- event:
		s.logger.Printf("sent order %d to session %s", order.ID, sessionID)
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

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event SSEEvent) error {
	data, err := json.Marshal(event.Data)

	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(w, "event: %s\n", event.Event); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}

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

func (s *MarketplaceServer) testSSEDelayed(sessionID string) {
	// Simulating an event (e.g., Order Processed) sent 5 seconds later

	time.AfterFunc(5*time.Second, func() {

		s.sessionsMu.RLock()

		currentSession, exists := s.sessions[sessionID]

		s.sessionsMu.RUnlock()

		// Verify the session hasn't been overwritten or closed in the meantime

		// if exists && currentSession == session {
		if exists {

			event := SSEEvent{
				Event: "order.created",
				Data: OrderEvent{
					OrderID:            strconv.FormatInt(7239414055129, 10),
					Name:               "#1026",
					ConfirmationNumber: "2LI8DGA0X",
					OrderNumber:        1026,
					Currency:           "USD",
					TotalPrice:         strconv.FormatFloat(60810.33, 'f', 2, 32),
					LineItems: []OrderItem{
						{
							ProductID: strconv.FormatInt(9865881256153, 10),
							VariantID: strconv.FormatInt(49175344709849, 10),
							Title:     "Azkomoly Box - M",
							Quantity:  3,
							Price:     strconv.FormatFloat(13144.23, 'f', 2, 32),
						},
						{
							ProductID: strconv.FormatInt(9865871786201, 10),
							VariantID: strconv.FormatInt(49175309091033, 10),
							Title:     "Azkomoly Box - S",
							Quantity:  2,
							Price:     strconv.FormatFloat(10688.82, 'f', 2, 32),
						},

						// FinancialStatus:    order.FinancialStatus,
					},
				},
			}

			// ===== Preemptive order =====

			// session.mu.Lock()

			// session.CompletedOrder = &OrderEvent{

			//  OrderID: sessionID,

			// }

			// session.mu.Unlock()

			// ===========================

			// Safely attempt to send to the channel

			select {

			case currentSession.Events <- event:

				// Event successfully queued for SSE listener

			default:

				s.logger.Printf("Warning: Channel full, event dropped for client %s", sessionID)

			}

		}

	})
}
