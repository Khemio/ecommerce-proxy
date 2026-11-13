# Roadmap and known issues

This is a small, focused improvement plan for making the internship prototype reliable and presentable. The items below are proposals, not completed features.

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
