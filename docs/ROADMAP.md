# Roadmap and known issues

This is a small, focused improvement plan for making the internship prototype reliable and presentable. The items below are proposals, not completed features. The baseline documentation reflects `main`; `dev` / `product-caching` also contain webhook/SSE work. See [development branch review](DEVELOPMENT_BRANCHES.md).

## P0 — correctness and safe operation

- [ ] Propagate outbound HTTP errors, non-2xx Shopify responses, GraphQL `errors` and `cartCreate.userErrors`; return consistent non-200 REST errors.
- [ ] Handle missing product and nil GraphQL data safely instead of asserting `.(map[string]any)` and risking a panic.
- [ ] Validate checkout lines: non-empty variant IDs, strictly positive quantities, non-empty cart, and bounded payload size.
- [ ] Replace duplicate `limit` parsing with one checked parse and bounds; reject invalid input with HTTP 400.
- [ ] Add outbound request timeouts and request-context propagation; avoid unbounded Shopify calls.
- [ ] Make `SHOPIFY_API_VERSION` control constructed endpoint URLs; eliminate the hardcoded version mismatch.
- [ ] Fail startup on missing/placeholder credentials rather than silently serving misconfigured routes.
- [ ] Audit ownership/publication rights and remove any real tokens or employer-specific internal details before sharing client and server code.

## P1 — tests and API quality

- [ ] Add table-driven unit tests for ID normalization and response transformation.
- [ ] Add handler tests using `net/http/httptest` and a mocked Shopify API.
- [ ] Add tests for malformed JSON, empty cart, invalid limit, missing products, timeouts and Shopify `userErrors`.
- [ ] Introduce typed, documented request/response structs and a stable JSON error envelope.
- [ ] Return consistent `Content-Type: application/json` on JSON responses, including health and errors.
- [ ] Constrain CORS to known client origins for production, especially if browser clients are expected.
- [ ] Ensure sensitive details cannot enter logs; consider request IDs and status/latency logging.
- [ ] Add CI to run `go test ./...`, `go vet ./...` and formatting checks.

## P2 — useful but optional

- [ ] Support cursor-based pagination for product listings.
- [ ] Expose multiple variants/images if the Flutter UI requires them.
- [ ] Decide whether the unused Admin API configuration/function should be removed.
- [ ] Add a production deployment guide and smoke-test checklist.
- [ ] Add optional integration tests gated behind explicit Shopify test credentials.

## Scope decisions

Do **not** add a database, authentication system, payment processing, large framework migration or microservice architecture merely for portfolio value. The immediate goal is a dependable thin integration server.

## Suggested definition of done

- Invalid requests and upstream failures never crash a handler and return a documented error.
- Product list, product detail and cart creation have automated happy-path/error-path tests.
- Deployment configuration and API version are aligned.
- The README matches tested local startup and contains no secrets.
- The related Flutter app is linked and can use the documented API without relying on undocumented response quirks.

## P0 — development branch SSE / webhook correctness

The following concern `dev` and `product-caching` at commit `21c5416`, not the current `main`:

- [ ] **Remove or explicitly gate `testSSEDelayed`**: every checkout currently queues a fabricated `order.created` notification after five seconds, regardless of payment.
- [ ] Verify and parse Shopify webhooks before acknowledging processing success; currently HTTP 200 is sent immediately after HMAC validation, before JSON parsing and session lookup.
- [ ] Define webhook retry/idempotency and duplicate-delivery behavior. Test invalid HMAC, malformed payloads, missing sessions and arrival-before-SSE races.
- [ ] Design session lifecycle for reconnects, expiry and multiple SSE listeners; the current buffered channel supports a single consumer and sessions live only in process memory.
- [ ] Resolve multi-instance deployment behavior: a webhook routed to a different Fly.io instance cannot find an in-memory session.
- [ ] Correct browser CORS configuration: wildcard `Access-Control-Allow-Origin: *` must not be combined with `Access-Control-Allow-Credentials: true` for credentialed requests.
- [ ] Add tests for real order completion, early webhook, client disconnect, heartbeat, session cleanup and graceful shutdown.

## P1 — product caching (planned; not present in pushed branch)

- [ ] Start with a small, concurrency-safe **in-memory TTL cache** for successful product-list and product-detail responses. Suggested initial TTL: 5–15 minutes, adjustable after observing update frequency.
- [ ] Key list responses by relevant query inputs (at least `limit`, and cursor when pagination is added); key detail responses by canonical Shopify product ID. Distinguish store/API version if configuration becomes multi-store.
- [ ] Prevent concurrent cache misses from producing redundant Shopify queries (e.g. `singleflight`).
- [ ] Do not cache failed queries, incomplete GraphQL responses, carts, checkout URLs or session-specific data.
- [ ] Test cold/hot fetches, TTL expiry, concurrent misses, failures, and updates. Add cache hit/miss counters.
- [ ] Consider Shopify product-update webhooks for invalidation **only after** basic TTL behavior is correct and the webhook path is robust.
- [ ] Remember that in-memory cache entries are per server instance and disappear on restart. Introduce a shared cache only if multi-instance operation requires it.

## P1 — inbound rate limiting (planned; not implemented)

- [ ] Add middleware using a token bucket (e.g. `golang.org/x/time/rate`) with a bounded, expiring per-client state map; do not let arbitrary IP keys grow indefinitely.
- [ ] Apply stricter limits to `POST /api/v1/checkout` than to product reads; use separate rules for SSE connections, health checks and authenticated Shopify webhooks.
- [ ] Return HTTP `429 Too Many Requests` and a useful `Retry-After` header. Add tests for bursts, sustained traffic and eviction.
- [ ] Carefully identify clients behind trusted proxies; do not blindly trust client-controlled `X-Forwarded-For`.
- [ ] In-memory limits are **per instance**. If Fly.io scales to multiple instances, consider rate limiting at the edge or in shared storage.
- [ ] Keep inbound abuse protection separate from Shopify upstream throttling, retries and GraphQL cost management.

## Implementation order

1. Fix existing error handling and unsafe checkout/session behavior.
2. Remove simulated order events; test the webhook/SSE workflow.
3. Add product caching with TTL and tests.
4. Introduce basic, observable per-client rate limiting.
5. Revisit shared caching and distributed limits only if deployment requires them.
