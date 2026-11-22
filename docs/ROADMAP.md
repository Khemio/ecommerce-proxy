# Roadmap and known limitations

This project is intentionally scoped as a small Go-to-Shopify integration backend for a Flutter e-commerce prototype. The immediate portfolio goal is a coherent, tested, honest single-instance implementation—not a comprehensive commerce platform.

## Implemented

- [x] Shopify Storefront GraphQL product list/detail and cart creation.
- [x] Checkout input validation (nonempty cart, valid lines, positive quantities, bounded JSON body).
- [x] HTTP status, JSON and GraphQL error propagation with outbound timeouts/context handling.
- [x] Checked catalog `limit` (1–100) and `200 []` for an empty catalog.
- [x] `SHOPIFY_API_VERSION` respected in endpoint construction.
- [x] Checkout session UUIDs, Shopify cart attribute correlation, verified order webhook, and SSE event delivery.
- [x] Removed fabricated development order events.
- [x] Basic duplicate-event suppression, reconnect handling, two-hour session TTL, and session cleanup.
- [x] Per-client token-bucket rate limiting and global/per-client SSE connection caps.
- [x] Bounded in-memory catalog cache with TTL, singleflight, and tests for expiry, failures, eviction, concurrency.
- [x] Automated `go test -race` checks and successful Docker build / local health smoke test.

## Before any public production deployment

- [ ] **Verify Shopify webhook semantics end to end.** `order.created` is not proof of successful payment. Check the actual subscription topic and design the client state transition accordingly.
- [ ] **Validate configuration at startup.** Reject missing/placeholder storefront and webhook secrets for production rather than allowing a superficially healthy server with unusable upstream integrations.
- [ ] **Review hosting lifecycle.** Fly's checked-in config auto-stops machines and keeps zero running, which is not aligned with in-memory session delivery. Decide whether to keep one instance permanently running or introduce shared, persistent session/event storage.
- [ ] **Review web security.** Limit CORS origins as appropriate, decide whether clients need authentication, and treat session IDs as bearer-like secrets; verify there is no direct bypass of trusted-proxy IP resolution.
- [ ] **Exercise a real Shopify sandbox/test store.** Validate checkout URL, order webhook signature, `go_session_id` propagation, webhook retry behavior, and Flutter SSE handling. Unit/integration mocks cannot establish these external contracts.
- [ ] **Reconcile deployment settings.** `fly.toml` has `PORT` rather than `SERVER_PORT`, an unused old `GO_VERSION` build argument, and automatic machine stopping. Review resource limits and ensure deployment secrets are configured.
- [ ] **Ensure publication permissions.** This began as internship work; verify that no employer-owned code, tokens, internal endpoints, or confidential details are published without authorization.

## Near-term maintenance and test quality

- [ ] Add happy-path tests for nonempty product transformations and successful checkout cart creation, plus invalid limit and Shopify `userErrors` cases.
- [ ] Test duplicate webhook requests concurrently, not only sequentially, and clarify behavior with two simultaneous SSE listeners.
- [ ] Add a uniform JSON error format and consistent response `Content-Type`; health currently has a JSON body without explicit JSON content type.
- [ ] Remove dead code, obsolete comments, duplicate configuration paths, and unused Admin API helpers if there is no planned use.
- [ ] Add GitHub Actions to run `gofmt` checks, `go test ./...`, `go test -race ./...` and `go vet ./...` on pull requests.
- [ ] Improve logging to include status/latency, with no secrets or unnecessary session identifiers.
- [ ] Add tests and constraints around oversized Shopify responses, pathological catalog shapes, and slow/aborted SSE clients.

## Future features (only when needed)

- [ ] Typed, stable product DTOs to replace dynamic map manipulation after the Shopify data contract is better understood.
- [ ] Cursor-based catalog pagination, multiple images/variants, and additional filtering/search if required by the Flutter UI.
- [ ] Cache product details; consider cache invalidation on product updates and simple hit/miss instrumentation.
- [ ] Persist checkout session/event state (for example Redis) and make webhook handling robust across multiple replicas and process restarts.
- [ ] Add a durable webhook queue, event IDs, replay/deduplication by webhook or order identity, and a stronger client acknowledgement protocol if reliable delivery becomes necessary.
- [ ] Add distributed rate limits or an edge/API gateway if multiple replicas or meaningful abuse traffic justify them.
- [ ] Evaluate a standalone reverse-proxy experiment separately; it is not required for the Shopify backend.

## Current operational assumptions

- One running Go server instance with modest traffic (original target: under 1,000 daily users).
- Sessions, cache, rate-limit buckets and SSE permits are in-memory and per process.
- One meaningful order notification per checkout session and one intended SSE consumer per session.
- Shopify is the source of truth for checkout, order lifecycle and payment.
- The server provides best-effort notifications, not a transaction ledger or exactly-once event guarantee.

## Completion criterion for the portfolio release

Documentation matches the merged source, setup and Docker commands work, automated tests pass, known limitations are explicit, and the related Flutter repository is linked. Do not claim verified real-world checkout or payment confirmation unless that has been tested with Shopify.
