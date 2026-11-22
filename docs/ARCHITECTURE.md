# Architecture and implementation notes

## Scope

The service is a small Go `net/http` integration layer between a Flutter storefront and Shopify. It is designed as a working prototype for modest traffic, with in-process state and no database, message broker, or custom reverse proxy. Shopify hosts checkout and handles payment.

## Component flow

```text
               Flutter storefront
                |             ^
        REST/JSON|             |SSE (order.created)
                v             |
       Go http.ServeMux --- session map / event channels
        |      |   |                     ^
        |      |   +-- rate limits ------|
        |      +------ catalog cache (TTL + singleflight)
        |                                |
        +-- Storefront GraphQL --> Shopify API
                                         |
                     Shopify order webhook
                       (verified HMAC)
                              |
                   webhook handler -> session/event
```

### Source files

- `main.go`: signal handling, server lifecycle and 5-second graceful-shutdown deadline; starts periodic session cleanup.
- `server.go`: environment variables, Shopify URLs, route registration, middleware and server timeouts.
- `api.go`: product/checkout REST endpoints, SSE streaming, HMAC-verified webhook handling.
- `shopify.go`: Storefront GraphQL queries and cart mutation, HTTP client and error propagation.
- `types.go`, `utils.go`: shared models, product/variant ID normalization, GraphQL node transformation, CORS and access logging.
- `session.go`, `session_cleanup.go`: mutex-protected in-memory sessions, expiry and cleanup.
- `rate_limit.go`, `sse_limit.go`: per-IP token buckets and connection-count limits.
- `product_cache.go`: bounded in-memory catalog response cache with `singleflight`.

## Product listing and cache

`GET /api/v1/products` validates `limit` in the range 1–100, then calls `fetchProductsCached` with the key `catalog:<limit>`. The Shopify response envelope is serialized into cache bytes and decoded independently for each caller, avoiding shared mutable product maps. `TransformGraphQLResponse` removes `edges`/`node` wrappers before the final REST JSON array is returned. An empty catalog is `200 []`.

Cache behavior:

- TTL: **60 seconds**; max **128 entries**; no entry larger than **256 KiB**.
- Successful complete catalog responses only. Missing `products.edges`, upstream failures and GraphQL failures do not populate cache.
- Concurrent misses for a key are coalesced by `singleflight`. Cache waiters can cancel their HTTP request independently of shared bounded upstream work.
- Simple earliest-expiration eviction when full. No persistent storage, cross-instance synchronization, explicit product-update invalidation or hit/miss metrics yet.
- `GET /api/v1/product/{product_id}` currently bypasses the cache.

The product queries intentionally request only the first variant and first image, rather than full cursor-based pagination.

## Checkout, webhooks and SSE

1. `POST /api/v1/checkout` validates 1–100 lines, positive quantities and nonempty variant IDs (1 MiB body limit).
2. The server generates a UUID, includes it as Shopify cart attribute `go_session_id`, calls `cartCreate`, and only records the session after a usable cart and checkout URL are returned.
3. Flutter retains `sessionId` and opens `GET /api/v1/events?sessionId=...`; the server sends a connection comment and heartbeat comments every 15 seconds.
4. Shopify sends an order webhook. The server limits the request body to 4 MiB, verifies its HMAC, parses `note_attributes`, and locates the matching session.
5. Under the session mutex, the handler records only the first completed order event; it then attempts a nonblocking send into a capacity-one event channel. The stored event handles the webhook-before-subscription case.
6. The SSE handler writes the `order.created` event and deletes the session after a successful write. A disconnect without successful delivery leaves the session available for reconnect until cleanup.

**Delivery contract:** This is a best-effort, single-process notification path, not a durable, exactly-once event system. An SSE write succeeding does not guarantee that the Flutter application consumed the event. Multiple simultaneous listeners for one session are not supported as a reliable broadcast, and a process restart destroys sessions and queued events. The session ID must be kept private; no additional user authentication is enforced on event subscriptions.

**Order versus payment:** The event name is `order.created`, reflecting an order notification, and does not prove the order is paid. If the Flutter UI needs paid-order confirmation, the subscribed Shopify topic/fields and client state transition must be updated accordingly.

## Abuse controls

- The product list/detail endpoints share one per-IP token bucket: **1 request/second**, burst **10**.
- Checkout has a separate per-IP token bucket: **1 request/30 seconds**, burst **3**.
- Each bucket retains at most **5,000** IP entries, removing entries inactive for more than 30 minutes during request-triggered cleanup.
- SSE is limited to **50 active streams globally** and **5 per client IP**. Connection permits are released on handler return.
- Ordinary request/checkout limits are skipped for health and webhook endpoints. The webhook is protected by HMAC verification and a body-size limit.
- IP identity uses `RemoteAddr` unless `TRUST_FLY_PROXY=true` explicitly enables the `Fly-Client-IP` header. A directly accessible listener must not trust client-supplied proxy headers.

All of these limits exist per running process; they are not a DDoS defense or a distributed quota system. Shared NAT addresses can cause independent users to share a limit.

## Networking and lifecycle

- Ordinary route handlers use a 15-second `http.TimeoutHandler`; webhook handler uses 4 seconds; SSE is excluded from those short timeouts.
- Shopify outbound HTTP requests use request contexts and a 10-second client timeout. Non-2xx statuses, GraphQL envelope errors, and JSON decoding errors propagate to handlers.
- `http.Server` has a 5-second read timeout and 60-second idle timeout; SSE streams can remain open until disconnect, expiry of the process, or event completion.
- Sessions expire after **2 hours**; a cleanup task runs approximately every **10 minutes**. Entries are in memory and are not synchronized between replicas.
- Wildcard CORS is currently enabled; the service does not implement client authentication or a uniform JSON error schema.

## Deployment implications

The repository includes a minimal Docker runtime and `fly.toml`. The current `fly.toml` permits machine auto-stopping with `min_machines_running=0`, which can invalidate in-memory sessions. For dependable checkout notifications, run a single continuously available instance or introduce shared state. Review the Fly settings and potential hosting costs before applying changes.

The checked-in Fly configuration sets `PORT=8080` but Go reads `SERVER_PORT` (also defaults to `8080`). Its `GO_VERSION` build argument is stale and unused by the current Dockerfile. Deployment credentials must be supplied as secrets. Fly client-IP resolution must only be enabled when the inbound path is restricted to trusted Fly Proxy.

## Test strategy

Local `httptest` integration tests cover input validation, upstream errors, an empty catalog, signed/unsigned webhooks, SSE arrival ordering/reconnects, session expiry and duplicate events, request limits, connection caps, cache hits/failures, expiry, eviction, and concurrent misses. Repeated `-race` runs exercise concurrent paths. The tests are primarily local and mock Shopify; they do not verify live Shopify subscriptions, paid-order semantics, or a deployed multi-instance topology.
