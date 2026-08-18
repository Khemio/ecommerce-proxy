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

	// Inject credentials safely (Fallback to defaults for local development)
	cfg := Config{
		Port:                   GetEnv("SERVER_PORT", "8080"),
		Environment:            GetEnv("ENV", "dev"),
		APIVersion:             GetEnv("SHOPIFY_API_VERSION", version),
		ShopifyDomain:          GetEnv("SHOPIFY_STORE_NAME", storeName),
		ShopifyAdminToken:      GetEnv("SHOPIFY_ADMIN_TOKEN", adminToken),
		ShopifyStorefrontToken: GetEnv("SHOPIFY_STOREFRONT_TOKEN", strorefrontToken),
	}
	cfg.AdminEndpoint = fmt.Sprintf("https://%s.myshopify.com/admin/api/%s/graphql.json", cfg.ShopifyDomain, version)
	cfg.StorefrontEndpoint = fmt.Sprintf("https://%s.myshopify.com/api/%s/graphql.json", cfg.ShopifyDomain, version)

	logger.Printf("Server will start on port %s...", cfg.Port)

	server := &MarketplaceServer{
		config: cfg,
		logger: logger,
	}

	// Multiplex routes cleanly using standard library features
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/health", server.HandleHealthCheck)
	mux.HandleFunc("GET /api/v1/products", server.HandleFetchVendorProducts)
	mux.HandleFunc("GET /api/v1/product/{product_id}", server.HandleFetchVendorProduct)
	mux.HandleFunc("POST /api/v1/checkout", server.HandleCheckout)

	// corsHandler := CorsMiddleware()

	srv := &http.Server{
		Addr: ":" + cfg.Port,
		// Addr:         "0.0.0.0:" + cfg.Port,
		Handler:      LoggingMiddleware(logger)(CorsMiddleware(mux)),
		WriteTimeout: 15 * time.Second,
		ReadTimeout:  15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	return srv, server
}
