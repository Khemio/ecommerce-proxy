# HTTP API reference

Base URL for local development: `http://localhost:8080`.

All routes are registered with Go's `http.ServeMux` method-aware patterns. Successful product/cart responses are JSON; error paths do **not** consistently return JSON. Examples below describe the intended shapes where upstream Shopify requests succeed.

## GET /api/v1/health

Returns HTTP 200 with body:

```json
{"status":"operational"}
```

The handler does not currently set a JSON Content-Type and does not probe Shopify connectivity.

## GET /api/v1/products

Optional query: `limit`, default `10`. A successful response is a JSON array of product objects; `edges` and `node` wrappers are removed. The GraphQL query requests each product's `id`, `title`, `description`, `productType`, first variant (`id`, `sku`, `price.amount`) and first image (`url`).

```sh
curl 'http://localhost:8080/api/v1/products?limit=5'
```

Shape (illustrative values):

```json
[
  {
    "id": "gid://shopify/Product/123",
    "title": "Example item",
    "description": "Description",
    "productType": "Accessories",
    "variants": [{"id": "gid://shopify/ProductVariant/456", "sku": "EX-1", "price": {"amount": "12.00"}}],
    "images": [{"url": "https://example.com/image.jpg"}]
  }
]
```

**Current limitation:** The handler attempts two separate parses of `limit`; malformed or out-of-range numbers are not safely rejected. Add a bounded integer validation step.

## GET /api/v1/product/{product_id}

Accepts either a numeric Shopify product ID or a full Shopify Product GID, for example `gid://shopify/Product/123` (URL-encode it in a path). Returns the single product object. The GraphQL query requests `title`, `description`, `productType`, first variant and first image; unlike the list query it does **not** request the product's own `id`.

```sh
curl 'http://localhost:8080/api/v1/product/123'
```

The server normalizes a plain ID to `gid://shopify/Product/{id}`.

**Current limitation:** An absent product or an upstream error can trigger a panic because the handler asserts that the transformed result is a map without first checking for nil. Error and not-found behavior needs implementing.

## POST /api/v1/checkout

Creates a Shopify cart from a JSON array of lines.

```sh
curl -X POST 'http://localhost:8080/api/v1/checkout' \
  -H 'Content-Type: application/json' \
  -d '[{"variantId":"gid://shopify/ProductVariant/456","quantity":1}]'
```

Each input element supports:

| Field | JSON type | Meaning |
| --- | --- | --- |
| `variantId` | string | Shopify ProductVariant GID or numeric ID; numeric IDs are normalized |
| `quantity` | integer | Quantity to add to the cart |

Success response shape (illustrative):

```json
{
  "id": "gid://shopify/Cart/example",
  "checkoutUrl": "https://example.myshopify.com/cart/c/example",
  "totalQuantity": 1
}
```

In `ENV=dev`, the handler appends `channel=headless-storefronts` to the checkout URL.

The backend creates the cart, not a completed payment or order. Payment occurs through Shopify's checkout URL.

**Current limitation:** The handler currently validates only whether JSON can be decoded; it does not enforce positive quantities, non-empty variant IDs, or sane array sizes. Shopify `userErrors` are parsed internally but not returned as an explicit failure to the client.

## Error handling and compatibility

HTTP 400 is currently used for invalid JSON in the checkout route and for an empty product path value. Other cases may return a misleading HTTP 200, empty data, or panic when Shopify or GraphQL fails. Clients should not rely on a stable error schema yet.

No authentication mechanism, pagination, rate limiting, or explicit request-body size limit is currently implemented.

The server uses a Storefront token in its outbound Shopify request header; callers do not send this token. Do not put Admin API tokens into a Flutter client.

The API currently has a `/api/v1` prefix but no versioned compatibility policy.
