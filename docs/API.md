# HTTP API reference

Base URL for local development: `http://localhost:8080`. All routes use Go's method-aware `http.ServeMux` patterns. JSON success responses are described below. Errors currently use a mixture of plain-text `http.Error` and a JSON-looking timeout body; there is no uniform JSON error envelope.

## GET /api/v1/health

**200 OK**:

```json
{"status":"operational"}
```

This is a process liveness check, not a Shopify connectivity check. The handler currently does not set `Content-Type: application/json` explicitly.

## GET /api/v1/products

`limit` is optional (default `10`), and must be an integer from **1 to 100**. Returns a JSON array of transformed product objects; **an empty catalog is `200 OK` with `[]`**, not `404`.

```bash
curl 'http://localhost:8080/api/v1/products?limit=5'
```

Example (illustrative values; the API requests the first variant and first image only):

```json
[
  {
    "id": "gid://shopify/Product/123",
    "title": "Sample item",
    "description": "Description",
    "productType": "Accessories",
    "vendor": "Example vendor",
    "availableForSale": true,
    "variants": [
      {"id":"gid://shopify/ProductVariant/456","sku":"SKU-1","price":{"amount":"12.00"}}
    ],
    "images": [{"url":"https://example.com/product.jpg"}]
  }
]
```

Shopify `edges` and `node` wrappers are removed before the REST response. A **60-second**, in-memory cache is keyed by `limit`. The cache holds up to **128** responses, with a **256 KiB** maximum for each cached response; successful responses exceeding that size are still returned but are not stored. Concurrent cache misses for a key are coalesced. Cart creation is never cached.

Errors: `400` for invalid `limit`; `502` when the Shopify request/response fails or a required product connection is missing; `429` for per-client request rate limiting.

## GET /api/v1/product/{product_id}

Returns one product. Accepts a numeric Shopify product ID or an encoded Shopify Product GID; plain IDs are normalized to `gid://shopify/Product/{id}`. This endpoint is **not cached** in the current implementation.

```bash
curl 'http://localhost:8080/api/v1/product/123'
```

Errors: `404` when Shopify returns no product; `502` for upstream or GraphQL failures; `429` for rate limits. The detail query requests the product title, description, product type, first variant and first image, but not its own `id`.

## POST /api/v1/checkout

Creates a Shopify cart and a server-side session used to correlate the later order webhook with the Flutter client. The server **does not process payments**.

```bash
curl -X POST 'http://localhost:8080/api/v1/checkout' \
  -H 'Content-Type: application/json' \
  -d '[{"variantId":"gid://shopify/ProductVariant/456","quantity":1}]'
```

Input is a JSON array of **1–100** cart lines. Each line must contain a nonblank `variantId` and positive integer `quantity`; unknown JSON fields are rejected. The body is capped at **1 MiB**. Numeric variant IDs are normalized to Shopify ProductVariant GIDs.

Example success (identifiers are illustrative):

```json
{
  "sessionId": "9efad3ad-55be-4d79-8260-6d2f1a0aa111",
  "id": "gid://shopify/Cart/example",
  "checkoutUrl": "https://example.myshopify.com/cart/c/example",
  "totalQuantity": 1,
  "attributes": [{"key":"go_session_id","value":"9efad3ad-55be-4d79-8260-6d2f1a0aa111"}]
}
```

The cart contains a `go_session_id` attribute. The checkout response includes the corresponding `sessionId`, which the client must retain for SSE subscription. In `ENV=dev`, a nonempty checkout URL gets the `channel=headless-storefronts` query parameter.

Errors: `400` for malformed or invalid input; `502` when Shopify request/GraphQL processing or cart creation fails; `429` for per-IP checkout rate limiting. The current API does not expose Shopify `userErrors` verbatim in the HTTP response.

## GET /api/v1/events?sessionId=...

Opens an **SSE** stream for an existing checkout session; this endpoint is not wrapped in the regular 15-second request timeout.

```bash
curl -N 'http://localhost:8080/api/v1/events?sessionId=9efad3ad-55be-4d79-8260-6d2f1a0aa111'
```

On connection, the server emits an SSE comment `: connected`. While the stream remains open, it sends heartbeat comments approximately every **15 seconds**. When a matching webhook is processed, it emits an event resembling:

```text
event: order.created
data: {"order_id":"42","order_name":"#1001","confirmation_number":"","order_number":1001,"total_price":"15.00","currency":"USD","line_items":[{"product_id":"100","variant_id":"200","title":"Sample item","quantity":1,"price":"15.00"}]}
```

If the webhook arrived before the SSE subscription, the session retains the event for delivery on connection. A client disconnect does not immediately delete the session; reconnecting can retrieve the stored event while the session is still present. Sessions expire after **2 hours**; cleanup runs every **10 minutes**. Sessions are deleted after a successfully written event.

Errors: `400` for a missing session ID; `404` for unknown/expired sessions; `429` when the same IP has too many SSE connections; `503` when all global SSE slots are occupied. The implementation is designed for **one consumer per checkout session** and does not support reliable broadcast to multiple subscribers or durable resume after restart.

**Semantics:** An `order.created` notification does not establish that the order was paid. Do not use it alone to mark a payment as successful.

## POST /api/v1/shopify/webhook

Inbound webhook endpoint intended for Shopify, not for the Flutter client. It reads the raw request body (up to **4 MiB**) and verifies the `X-Shopify-Hmac-Sha256` signature with `SHOPIFY_SECRET_TOKEN` before decoding the JSON order payload. It looks for `go_session_id` in Shopify's `note_attributes`.

On a matching in-memory session, the handler stores an `order.created` event and offers it to the session's buffered SSE channel. A repeated event for an already-completed session is acknowledged without replacing the event. Missing/expired sessions and orders without `go_session_id` are logged and implicitly acknowledged with HTTP `200`.

| Status | Meaning |
| --- | --- |
| `200` | Accepted/handled, or ignored because no matching session was available |
| `400` | Request-body read error or signed payload with invalid JSON |
| `401` | Invalid/missing HMAC signature |
| `413` | Payload exceeds 4 MiB |

The webhook route has a **4-second** handler timeout. It is exempt from general per-IP request limiting. Its events are not persisted to a durable queue.

## Request and connection limits

| Scope | Implementation |
| --- | --- |
| Product list and detail (shared per-client bucket) | 1 token/second, burst 10; excess returns `429` |
| Checkout (separate per-client bucket) | 1 token/30 seconds, burst 3; excess returns `429` |
| SSE globally | At most 50 concurrent streams per process; excess returns `503` |
| SSE per IP | At most 5 concurrent streams; excess returns `429` |
| Health and Shopify webhook | No general per-IP token-bucket limit |

Rate-limited responses include a `Retry-After` header. Per-IP request-limit state is bounded to 5,000 entries per bucket and inactive entries are evicted during request-triggered cleanup. Limits are in-process, **not distributed across instances**.

Client identity comes from `RemoteAddr` by default. If `TRUST_FLY_PROXY=true`, the server uses `Fly-Client-IP`; this setting is only appropriate when the server is reachable exclusively through trusted Fly Proxy.

## Compatibility and limitations

- The `/api/v1` prefix exists, but no formal backwards-compatibility policy has been established.
- REST errors are not yet a consistently structured JSON type.
- The catalog does not implement cursor pagination or full variant/image coverage.
- Sessions are memory-resident; retries, multiple server instances, and process restarts can disrupt order-event delivery.
- There is no frontend authentication/authorization; treat checkout-session IDs as sensitive bearer-like values.
