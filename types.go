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

	sessionsMu sync.RWMutex
	sessions   map[string]*Session
}

type Session struct {
	ID string
	// ClientID  string
	CartToken string

	CreatedAt time.Time

	// Events sent from the Shopify webhook handler
	// to the SSE handler.
	// Events chan OrderEvent
	Events chan SSEEvent

	// Protects CompletedOrder.
	mu sync.Mutex

	// Set when the Shopify webhook arrives.
	// This handles the race where the webhook arrives
	// before Flutter opens the SSE connection.
	CompletedOrder *SSEEvent
}

type OrderEvent struct {
	OrderID            string      `json:"order_id"`
	Name               string      `json:"order_name"`
	ConfirmationNumber string      `json:"confirmation_number"`
	OrderNumber        int         `json:"order_number"`
	TotalPrice         string      `json:"total_price"`
	Currency           string      `json:"currency"`
	LineItems          []OrderItem `json:"line_items"`
	// FinancialStatus    string `json:"financial_status"`
}

type OrderItem struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Title     string `json:"title"`
	Quantity  int    `json:"quantity"`
	Price     string `json:"price"`
}

type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// Temp struct 'YourSessionID' would be CartId
type ShopifyOrderWebhook struct {
	ID                 int64              `json:"id"`
	AdminGraphQLID     string             `json:"admin_graphql_api_id"`
	Name               string             `json:"name"`
	ConfirmationNumber string             `json:"confirmation_number"`
	OrderNumber        int                `json:"order_number"`
	FinancialStatus    string             `json:"financial_status"`
	TotalPrice         string             `json:"total_price"`
	Currency           string             `json:"currency"`
	LineItems          []ShopifyLineItem  `json:"line_items"`
	Attributes         []ShopifyAttribute `json:"note_attributes"`
}

func (o ShopifyOrderWebhook) SessionID() string {
	for _, attr := range o.Attributes {
		if attr.Key == "go_session_id" {
			return attr.Value
		}
	}

	return ""
}

type ShopifyAttribute struct {
	Key   string `json:"name"`
	Value string `json:"value"`
}

type ShopifyLineItem struct {
	ProductID int64  `json:"product_id"`
	VariantID int64  `json:"variant_id"`
	Title     string `json:"title"`
	Name      string `json:"name"`
	Quantity  int    `json:"quantity"`
	Price     string `json:"price"`
	SKU       string `json:"sku"`
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
