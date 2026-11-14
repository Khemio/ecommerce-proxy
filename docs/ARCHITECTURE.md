# Architecture and implementation notes

## Request flow

```text
Flutter (HTTP/JSON)
  -> Go net/http ServeMux
     -> LoggingMiddleware -> CorsMiddleware
        -> REST handler (api.go)
           -> Shopify function (shopify.go)
              -> GraphQL POST to Shopify Storefront API
           -> GraphQL edge/node transformation
        -> JSON response
```

All registered public handlers use the Storefront API. `sendAdminRequest` and the Admin token/endpoint exist in the code but are not used by these handlers.

## Responsibilities

- `main.go`: starts `http.Server` in a goroutine, waits for SIGINT/SIGTERM and attempts a graceful shutdown with a five-second deadline.
- `server.go`: reads environment variables, creates `MarketplaceServer`, registers four routes, configures CORS/logging middleware and server timeouts (15-second read/write, 60-second idle).
- `api.go`: parses request parameters and checkout JSON, calls Shopify helpers, transforms and serializes results.
- `shopify.go`: builds product list/detail GraphQL queries and `cartCreate` mutation, attaches the Storefront token, and performs HTTP POST requests.
- `types.go`: models input line items, Shopify response envelopes and service configuration.
- `utils.go`: normalizes ProductVariant IDs; removes GraphQL `edges`/`node` wrappers; handles CORS and access logging.

## Data mapping

Shopify connections typically contain `edges: [{node: {...}}]`. `TransformGraphQLResponse` converts such structures into arrays of the underlying nodes. This is used to simplify the output shape expected by the Flutter client.

The product query only asks Shopify for the first variant and first image, so this is not a full catalog/variant API. Product listing currently requests a first page without cursor-based pagination.

## Operational notes

- `SHOPIFY_STORE_NAME` becomes the `<store>.myshopify.com` host. Build endpoint URLs carefully and validate configuration.
- `SHOPIFY_API_VERSION` is stored on `Config`, but endpoint URLs use the package-level `version` instead. This is a configuration bug.
- `ENV=dev` modifies the checkout URL by adding `channel=headless-storefronts`.
- `CorsMiddleware` allows `*` origins and broad request methods/headers.
- `LoggingMiddleware` records remote address, method, path and elapsed time, but not status codes or request IDs.
- Outbound requests create a fresh `http.Client` and omit explicit timeouts/context propagation.
- Shopify HTTP error status codes, GraphQL `errors` and cart `userErrors` do not currently propagate to appropriate REST error responses.
- There are currently no test files in the repository.

## Suggested boundaries as the application grows

Introduce a Shopify client/interface for HTTP transport and GraphQL operations, typed DTOs for REST requests/responses, a consistent API error type, and independent handler tests using `httptest`. Keep cart/checkout business rules out of HTTP handlers.

This is a suggested future direction, not the implemented architecture.
