# ecommerce-proxy

A small Go HTTP service connecting a Flutter storefront to Shopify. It exposes a simpler JSON catalog API, creates Shopify carts, and forwards order-creation notifications to the client through Server-Sent Events (SSE). Originally developed as an internship prototype; now maintained as a portfolio project.

**Status:** Functional, tested prototype for a small, single-instance deployment. It is **not** a production-hardened commerce gateway. Shopify handles hosted checkout and payment; this server neither collects payment details nor confirms that an order has been paid.

## Features

- Catalog listing (`limit` defaults to 10, maximum 100) and individual product lookup through the Shopify Storefront GraphQL API.
- Normalization of Shopify product/variant IDs and flattening of GraphQL `edges`/`node` wrappers into simpler JSON.
- Cart creation with validated items, a generated checkout-session ID, and a Shopify-hosted checkout URL.
- HMAC-verified Shopify order webhooks and `order.created` SSE notifications, including notification storage when the webhook arrives before the client subscribes.
- In-process checkout-session tracking with expiration, basic duplicate-event suppression, and disconnect/reconnect handling.
- Per-client token-bucket request limits, global and per-client SSE connection caps, and bounded in-memory catalog caching with `singleflight`.
- Graceful HTTP shutdown, request logging, automated Go tests, multi-stage Docker image, and a Fly.io configuration.

## Request flow

```text
Flutter client -- HTTP/JSON --> Go API -- GraphQL/HTTPS --> Shopify Storefront API
     |                            |                               |
     |    cart + checkout URL <-- | <--------- cartCreate ----------+
     |                            |
     +----- GET /events (SSE) --> | <-- signed order webhook -- Shopify
     |                            |
     <----- order.created event --+
```

`order.created` means an order-creation notification was received. It must not be interpreted as proof of successful payment without checking the Shopify event topic and payment status.

## Requirements

- **Go 1.26+** (`go.mod` declares Go 1.26.0).
- A Shopify store with Storefront API access and token.
- The appropriate Shopify webhook subscription and signing secret to exercise order notifications.
- Optional: Docker for a containerized build.

## Configuration

Environment variables are read from the process environment; `.env` files are **not** loaded automatically. Do not commit real credentials.

| Variable | Default | Purpose |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | HTTP listen port |
| `ENV` | `dev` | In `dev`, appends `channel=headless-storefronts` to nonempty checkout URLs |
| `SHOPIFY_STORE_NAME` | `test-store` | Store subdomain, without `.myshopify.com` |
| `SHOPIFY_API_VERSION` | `2026-07` | API version used in Shopify endpoint URLs |
| `SHOPIFY_STOREFRONT_TOKEN` | placeholder | Storefront GraphQL access token |
| `SHOPIFY_SECRET_TOKEN` | placeholder | Secret used to verify `X-Shopify-Hmac-Sha256` on incoming webhooks |
| `SHOPIFY_ADMIN_TOKEN` | placeholder | Currently configured but unused by public routes |
| `TRUST_FLY_PROXY` | `false` | When `true`, uses `Fly-Client-IP` for per-client rate limits; use only behind trusted Fly Proxy |

**Security warning:** The code currently permits placeholder tokens at startup. A running health endpoint does not mean Shopify or webhook credentials have been validated. Set real credentials before exercising upstream operations and do not expose the default configuration as a production service.

### Run locally (PowerShell)

```powershell
$env:SERVER_PORT = '8080'
$env:ENV = 'dev'
$env:SHOPIFY_STORE_NAME = 'your-store-subdomain'
$env:SHOPIFY_STOREFRONT_TOKEN = 'your-storefront-token'
$env:SHOPIFY_SECRET_TOKEN = 'your-webhook-signing-secret'
go run .
```

In a second terminal:

```powershell
curl.exe -i http://localhost:8080/api/v1/health
```

These variables are examples, not working credentials. Use an environment loader or secret manager for actual values. Leave `TRUST_FLY_PROXY` unset for local development.

### Tests

```powershell
gofmt -w *.go
go test -race -count=1 ./...
go vet ./...
```

The suite uses local `httptest` servers to verify HTTP behavior, checkout validation, webhook signatures, SSE session transitions, connection limits, rate limiting, and cache behavior. It does not constitute an end-to-end test against a real Shopify store.

### Docker

```powershell
docker build -t ecommerce-proxy:local .
docker run --rm -p 8080:8080 ecommerce-proxy:local
```

The second command is suitable for a health check without Shopify credentials. Supply tokens securely for Shopify requests. The multi-stage Dockerfile uses a Go 1.26 builder and a minimal `scratch` runtime image.

## HTTP API

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/v1/health` | Process health |
| GET | `/api/v1/products?limit=10` | List products; returns `[]` for an empty catalog |
| GET | `/api/v1/product/{product_id}` | Get one product |
| POST | `/api/v1/checkout` | Create Shopify cart and checkout session |
| GET | `/api/v1/events?sessionId=...` | Subscribe to order notifications over SSE |
| POST | `/api/v1/shopify/webhook` | Receive HMAC-verified Shopify order webhook |

See [API.md](docs/API.md) for request shapes, event data, error statuses, and limits.

## Project layout

| File | Responsibility |
| --- | --- |
| `main.go`, `server.go` | Startup/shutdown, configuration, routes and middleware |
| `api.go`, `shopify.go` | REST/SSE/webhook handlers and Shopify GraphQL client |
| `types.go`, `utils.go` | Request/response types, ID conversion, data transformation, logging and CORS |
| `session.go`, `session_cleanup.go` | Mutex-protected session storage and TTL cleanup |
| `rate_limit.go`, `sse_limit.go` | Token-bucket request limits and SSE concurrency limits |
| `product_cache.go` | Bounded TTL catalog cache with concurrent-miss coalescing |
| `*_test.go` | Handler, concurrency, limit and cache tests |
| `Dockerfile`, `fly.toml` | Container and Fly.io deployment configuration |

Read [ARCHITECTURE.md](docs/ARCHITECTURE.md) for details and [ROADMAP.md](docs/ROADMAP.md) for current limitations and future work.

## Deployment caveats

- **Single instance only for dependable notifications.** Sessions and limits exist in process memory. A restart or a webhook routed to another instance can lose session association.
- The checked-in `fly.toml` still allows automatic stopping with zero machines kept running. That is **not** aligned with dependable in-memory checkout notifications; review it before deployment.
- `TRUST_FLY_PROXY=true` is safe only for requests known to arrive through trusted Fly Proxy. Never trust arbitrary client-supplied forwarding headers on a directly exposed port.
- No client authentication or authorization is implemented. Session IDs effectively grant access to the associated order event and should not be publicly disclosed.
- Browsers may access the API from any origin due to wildcard CORS. The service is not yet a security-reviewed public deployment.

## Related frontend

[ecommerce-flutter](https://github.com/Khemio/ecommerce-flutter) is the companion Flutter storefront prototype. The Go service and Flutter client are kept in separate repositories with independent release cycles.
