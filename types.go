package main

import (
	"log"
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
}

// MarketplaceServer wraps dependencies cleanly for our API endpoints
type MarketplaceServer struct {
	config Config
	logger *log.Logger
}

type Item struct {
	VariantID string `json:"variantId"`
	Quantity  int    `json:"quantity"`
}

// GraphQL Types
// type Product struct {
// 	// ID    string `json:"id"`
// 	// Title string `json:"title"`
// 	ProductData map[string]any `json:`
// }

//	type ProductEdge struct {
//		Node Product `json:"node"`
//	}
// type ProductEdge struct {
// 	Node map[string]any `json:"node"`
// }

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

type CartResult struct {
	Id          string `json:"id"`
	CheckoutUrl string `json:"checkoutUrl"`
	Quantity    int    `json:"totalQuantity"`
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
