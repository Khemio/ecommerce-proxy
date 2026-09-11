package main

import (
	"log"
	"sync"
	"time"
)

// Config holds our operational backend environment variables
type Config struct {
	Port                   string
	Environment            string
	APIVersion             string
	ShopifyDomain          string
	ShopifyAdminToken      string
	ShopifyStorefrontToken string
	AdminEndpoint          string
	StorefrontEndpoint     string
	ShopifySecretToken     string
}

// MarketplaceServer wraps dependencies cleanly for our API endpoints
type MarketplaceServer struct {
	config Config
	logger *log.Logger

	sessionsMu   sync.RWMutex
	sessions     map[string]*Session
	cartSessions map[string]string //? maybe not needed
}

type Session struct {
	ID string
	// ClientID  string
	CartToken string

	CreatedAt time.Time

	// Events sent from the Shopify webhook handler
	// to the SSE handler.
	Events chan OrderEvent

	// Protects CompletedOrder.
	mu sync.Mutex

	// Set when the Shopify webhook arrives.
	// This handles the race where the webhook arrives
	// before Flutter opens the SSE connection.
	CompletedOrder *OrderEvent
}

type OrderEvent struct {
	OrderID string `json:"orderId"`
}

// Temp struct 'YourSessionID' would be CartId
type ShopifyOrderWebhook struct {
	ID int64 `json:"id"`
	// YourSessionID string `json:"your_session_id"`
	CartID string `json:"cart_id"`
}

type Item struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

type ProductConnection struct {
	// Edges []ProductEdge `json:"edges"`
	Edges []map[string]any `json:"edges"`
}

type QueryResult struct {
	Products ProductConnection `json:"products"`
}

type ProductQueryResult struct {
	Product map[string]any `json:"product"`
}

type CheckoutResponse struct {
	SessionId string `json:"sessionId"`
	// Cart      CartResult `json:"cart"`
	Id          string              `json:"id"`
	CheckoutUrl string              `json:"checkoutUrl"`
	Quantity    int                 `json:"totalQuantity"`
	Attributes  []map[string]string `json:"attributes"` //? Probably not necessary
}

// type CheckoutAttributes struct {

// }

// TODOD: Find a better way to parse attributes
type CartResult struct {
	Id          string              `json:"id"`
	CheckoutUrl string              `json:"checkoutUrl"`
	Quantity    int                 `json:"totalQuantity"`
	Attributes  []map[string]string `json:"attributes"`
	// Attributes  CheckoutAttributes `json:"attributes"`
}

type CartCreateQueryResult struct {
	// Cart       map[string]any `json:"cart"`
	Cart       CartResult `json:"cart"`
	UserErrors []struct {
		Message string `json:"message"`
	} `json:"userErrors"`
}

type CheckoutQueryResult struct {
	CartCreate CartCreateQueryResult `json:"cartCreate"`
}

type GraphQLResponse[T any] struct {
	Data   T `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}
