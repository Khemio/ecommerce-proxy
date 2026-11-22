# Development branch history (archived)

This file is a brief historical note. The original review, written on **2026-11-14**, compared `main`, `dev`, `product-caching`, and Fly.io experiment branches before the CV-readiness work.

At that time, `dev` included checkout-session correlation, Shopify order webhooks and SSE, but still had fabricated order events, incomplete error handling, and no actual product cache or rate limiter. The `product-caching` branch name was aspirational: it pointed to the same commit as `dev`.

These observations are **not the current implementation status**. The later `fix/cv-readiness` branch added tests, removed fabricated SSE events, improved upstream errors, introduced token-bucket limits and SSE concurrency caps, and implemented in-memory catalog caching. It also merged the original `main` history for integration.

For the current feature set, consult [README](../README.md), [API reference](API.md), [architecture](ARCHITECTURE.md) and [roadmap](ROADMAP.md). The exact branch topology may change as this work is merged into `main`.
