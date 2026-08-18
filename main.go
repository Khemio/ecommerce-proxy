package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	// shopify "github.com/r0busta/go-shopify-graphql/v9"
)

var storeName = "test-store-ft9adler"
var adminToken = "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"
var strorefrontToken = "382fc1bd63be2d32674b74bfc126cc18"
var version = "2026-07"

func main() {

	srv, server := MakeServer()

	// Concurrency Strategy: Graceful shutdown loop
	go func() {
		server.logger.Printf("Server initializing on port %s...", server.config.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			server.logger.Fatalf("Critical system boot failure: %v", err)
		}
	}()

	// Listen for OS interrupt signals cleanly
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	server.logger.Println("Shutdown signal received. Packaging remaining threads...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		server.logger.Fatalf("Forced shutdown encountered errors: %v", err)
	}
	server.logger.Println("Server safely offline.")
}
