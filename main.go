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

var version = "2026-07"

func main() {

	srv, server := MakeServer()

	// Context controlling background server tasks.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start background session cleanup.
	go server.StartSessionCleanup(ctx)

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
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		server.logger.Fatalf("Forced shutdown encountered errors: %v", err)
	}
	server.logger.Println("Server safely offline.")
}
