package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *MarketplaceServer) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"operational"}`))
}

func (s *MarketplaceServer) HandleFetchVendorProduct(w http.ResponseWriter, r *http.Request) {
	// Extract path parameter using the standard library method
	productId := r.PathValue("product_id")
	if productId == "" {
		http.Error(w, "Product ID required", http.StatusBadRequest)
		return
	}

	graphQLResp, err := FetchProduct(r.Context(), &s.config, productId)
	if err != nil {
		s.logger.Printf("FetchProduct failed: %v", err)
		http.Error(w, "Shopify request failed", http.StatusBadGateway)
		return
	}

	if graphQLResp.Data.Product == nil {
		http.Error(w, "Product not found", http.StatusNotFound)
		return
	}

	product := TransformGraphQLResponse(graphQLResp.Data.Product).(map[string]any)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(product); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleFetchVendorProducts(w http.ResponseWriter, r *http.Request) {
	limit := 10

	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			http.Error(w, "limit must be between 1 and 100", http.StatusBadRequest)
			return
		}
		limit = parsed
	}

	// graphQLResp, err := FetchProducts(r.Context(), &s.config, limit)
	graphQLResp, err := s.fetchProductsCached(r.Context(), limit)
	if err != nil {
		s.logger.Printf("FetchProducts failed: %v", err)
		http.Error(w, "Shopify request failed", http.StatusBadGateway)
		return
	}

	products := make(
		[]map[string]any,
		0,
		len(graphQLResp.Data.Products.Edges),
	)

	for _, edge := range graphQLResp.Data.Products.Edges {
		product, ok := TransformGraphQLResponse(edge).(map[string]any)
		if !ok {
			s.logger.Printf("Unexpected product data: %T", edge)
			http.Error(w, "Invalid Shopify response", http.StatusBadGateway)
			return
		}

		products = append(products, product)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(products); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	var items []Item

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&items); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		http.Error(w, "Unexpected data after cart items", http.StatusBadRequest)
		return
	}
	if len(items) == 0 || len(items) > 100 {
		http.Error(w, "Cart must contain between 1 and 100 lines", http.StatusBadRequest)
		return
	}
	for _, item := range items {
		if strings.TrimSpace(item.VariantID) == "" || item.Quantity <= 0 {
			http.Error(w, "Each cart line requires a variantId and positive quantity", http.StatusBadRequest)
			return
		}
	}

	sessionID := uuid.NewString()

	graphQLResp, err := CreateCartAndCheckout(
		r.Context(),
		&s.config,
		items,
		sessionID,
	)
	if err != nil {
		s.logger.Printf("CreateCartAndCheckout failed: %v", err)
		http.Error(w, "Shopify request failed", http.StatusBadGateway)
		return
	}

	cart := graphQLResp.Data.CartCreate.Cart
	if len(graphQLResp.Errors) > 0 || len(graphQLResp.Data.CartCreate.UserErrors) > 0 || cart.Id == "" || cart.CheckoutUrl == "" {
		http.Error(w, "Unable to create Shopify cart", http.StatusBadGateway)
		return
	}

	session := &Session{
		ID: sessionID,
		// CartToken: cartToken,

		CreatedAt: time.Now(),

		// Small buffer protects us from the webhook
		// arriving before SSE starts.
		Events: make(chan SSEEvent, 1),
	}

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
			s.logger.Printf("Failed writing stored SSE event: %v", err)
			return
		}

		s.logger.Printf(
			"Completed order sent to session: %s",
			sessionID,
		)

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

const maxWebhookBodySize = 4 << 20 // 4 MiB

func (s *MarketplaceServer) HandleShopifyWebhook(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodySize)
	defer r.Body.Close()

	body, err := io.ReadAll(r.Body)

	if err != nil {
		var maxErr *http.MaxBytesError

		if errors.As(err, &maxErr) {
			http.Error(w, "webhook payload too large",
				http.StatusRequestEntityTooLarge)
			return
		}

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

	var order ShopifyOrderWebhook

	if err := json.Unmarshal(body, &order); err != nil {
		s.logger.Println("Failed to parse response: ", err)
		http.Error(w, "invalid webhook JSON", http.StatusBadRequest)
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

	if session.CompletedOrder != nil {
		session.mu.Unlock()
		s.logger.Printf(
			"Order already recorded for session %s",
			sessionID,
		)
		w.WriteHeader(http.StatusOK)
		return
	}

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

	w.WriteHeader(http.StatusOK)
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
