# Development branches: SSE/webhooks and proposed cache/rate limiting

> Inspected on 2026-11-14. This document describes code on `dev` and `product-caching` at commit `21c5416850344b998eabd236abf8aa0182ff08e6`. The `README.md` and `API.md` document `main` until these changes are integrated.

## Branch inventory

| Branch | State compared with `main` | Notes |
| --- | --- | --- |
| `main` | baseline | Four REST endpoints |
| `dev` | five commits ahead, two behind | Checkout session / webhook / SSE work |
| `product-caching` | same head as `dev` | Branch name suggests planned caching, but no implemented cache found |
| `flyio-new-files` | two commits ahead | Dockerfile and Fly.io changes |
| `flyio-scale-from-ui` | behind `main` | Earlier Fly.io scaling work |
| `docs/project-documentation` | docs branch | Documents baseline and this branch review |

The `dev` and `product-caching` branches have identical committed contents at inspection time. Their divergence from `main` also includes differences from the Fly.io commits merged to `main`; do not merge without reviewing deployment changes.

## Changes present on `dev`

- Checkout creates a server-generated UUID session identifier and includes `go_session_id` as a Shopify cart attribute.
- A new `GET /api/v1/events?sessionId=<uuid>` SSE endpoint streams `order.created` notifications, with 15-second heartbeat comments.
- A new `POST /api/v1/shopify/webhook` handler verifies `X-Shopify-Hmac-Sha256` using `SHOPIFY_SECRET_TOKEN`, parses order information and associates orders with an in-memory checkout session.
- The server keeps sessions in a mutex-protected map, queues events via buffered channels and cleans up sessions older than two hours every ten minutes.
- The checkout response includes `sessionId` alongside cart ID, checkout URL, quantity and attributes.
- `SHOPIFY_SECRET_TOKEN` is a new environment variable for webhook HMAC validation.
- Standard HTTP and webhook routes use `http.TimeoutHandler`; SSE is excluded from its short timeout.

### Development-only request flow

```text
Flutter -> POST /api/v1/checkout -> Go creates UUID -> Shopify cartCreate
                                      |
                                      +-> cart attribute go_session_id
                                      +-> in-memory Session
                                      +-> return sessionId + checkoutUrl

Flutter -> GET /api/v1/events?sessionId=... -> SSE connection
Shopify -> POST /api/v1/shopify/webhook -> HMAC verified -> order mapped to session
                                                        -> order.created SSE event
```

The design can buffer an event arriving before Flutter connects. A received event is also stored in `CompletedOrder`.

## Important limitations in the current committed code

1. `HandleCheckout` calls `testSSEDelayed(sessionID)` unconditionally, which generates a **fake paid-order-looking event** after five seconds. Disable/remove before relying on events as transaction confirmations.
2. The webhook handler sends HTTP 200 after validating HMAC but before it checks JSON validity, session existence or processing success.
3. Sessions are stored in process memory; multi-instance routing and restarts can break webhook-to-SSE association.
4. Each session has a single event channel; multiple SSE listeners can race to consume it. Reconnect handling needs explicit semantics.
5. CORS returns both `Access-Control-Allow-Origin: *` and `Access-Control-Allow-Credentials: true`, which is not valid for browser credentialed requests.
6. The Shopify HTTP client still has no explicit timeout/request context, GraphQL errors are not correctly propagated, and checkout validation remains incomplete.
7. There are no committed product-cache structures/lookup logic, no rate-limit middleware, and no tests in the inspected tree.

## Proposed caching strategy

A small single-instance, in-memory TTL cache is an appropriate first implementation because catalog data changes infrequently and product reads repeat. Cache only successful complete representations. Use canonical product IDs for detail lookups and include list query parameters in list cache keys. Prevent duplicate concurrent misses using `singleflight`. Start with a 5–15 minute TTL, verify behavior through tests/metrics, then optionally use Shopify product-update webhooks for invalidation.

Do not cache cart creation, checkout URLs, session state or errors. A shared cache is only justified if the deployment runs multiple instances and benefits from consistent cache behavior.

## Proposed inbound rate limiting

Start with Go middleware implementing per-client token buckets (e.g. `golang.org/x/time/rate`) with eviction of inactive entries. Allow short bursts while capping sustained traffic. Checkout should have tighter limits than GET product browsing. Health, SSE and authenticated Shopify webhook traffic need separate treatment. Respond with HTTP 429 and `Retry-After`.

Behind Fly.io or another reverse proxy, establish a trusted method to identify originating client IPs. Per-process limiters do not enforce a single global threshold when instances scale; shared/edge controls may be needed. Shopify upstream rate/cost limits are a different concern from inbound API abuse.

The roadmap intentionally avoids introducing Redis or other infrastructure solely for a prototype.

## Integration path

Prioritize webhook/SSE correctness first, then tests, then caching and rate limiting. Review `dev` against `main` before merging because deployment changes differ. Keep the existing documentation accurate for the branch it describes, and update the main API reference when the new routes actually land on `main`.
