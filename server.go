package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

func MakeServer() (*http.Server, *MarketplaceServer) {
	logger := log.New(os.Stdout, "[MARKETPLACE-API] ", log.LstdFlags|log.Lshortfile)

	cfg := Config{
		Port:                   GetEnv("SERVER_PORT", "8080"),
		Environment:            GetEnv("ENV", "dev"),
		APIVersion:             GetEnv("SHOPIFY_API_VERSION", version),
		ShopifyDomain:          GetEnv("SHOPIFY_STORE_NAME", "test-store"),
		ShopifyAdminToken:      GetEnv("SHOPIFY_ADMIN_TOKEN", "admin-tocken"),
		ShopifyStorefrontToken: GetEnv("SHOPIFY_STOREFRONT_TOKEN", "store-front-token"),
		ShopifySecretToken:     GetEnv("SHOPIFY_SECRET_TOKEN", "secret-token"),
	}
	cfg.AdminEndpoint = fmt.Sprintf("https://%s.myshopify.com/admin/api/%s/graphql.json", cfg.ShopifyDomain, version)
	cfg.StorefrontEndpoint = fmt.Sprintf("https://%s.myshopify.com/api/%s/graphql.json", cfg.ShopifyDomain, version)

	logger.Printf("Server will start on port %s...", cfg.Port)

	server := &MarketplaceServer{
		config: cfg,
		logger: logger,

		sessions: make(map[string]*Session),
	}

	// Multiplex routes cleanly using standard library features
	mux := http.NewServeMux()

	// Regular HTTP endpoints get a strict 15-second cutoff
	mux.Handle("GET /api/v1/health", withTimeout(15*time.Second, server.HandleHealthCheck))
	mux.Handle("GET /api/v1/products", withTimeout(15*time.Second, server.HandleFetchVendorProducts))
	mux.Handle("GET /api/v1/product/{product_id}", withTimeout(15*time.Second, server.HandleFetchVendorProduct))
	mux.Handle("POST /api/v1/checkout", withTimeout(15*time.Second, server.HandleCheckout))

	// Webhooks might need slightly longer depending on processing, e.g., 30s
	mux.Handle("POST /api/v1/shopify/webhook", withTimeout(30*time.Second, server.HandleShopifyWebhook))

	mux.HandleFunc("GET /api/v1/events", server.HandleSSE)

	// corsHandler := CorsMiddleware()

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		// Addr:         "0.0.0.0:" + cfg.Port,
		Handler:      LoggingMiddleware(logger)(CorsMiddleware(mux)),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  60 * time.Second,
	}

	return srv, server
}

func withTimeout(timeout time.Duration, next http.HandlerFunc) http.Handler {
	return http.TimeoutHandler(next, timeout, `{"error": "request timed out"}`)
}
