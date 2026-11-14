# ecommerce-proxy

A small Go HTTP API that connects a Flutter e-commerce client to Shopify's Storefront GraphQL API. It exposes product browsing and cart creation through a simpler JSON interface. Originally developed as part of an internship project.

> **Project status:** Working prototype / portfolio project. The API is not production-hardened: upstream and GraphQL errors currently need proper handling, inputs need validation, and integration tests are not included.

## Features

- Fetch a list of Shopify products, including their first variant and image.
- Fetch an individual product by Shopify Product GID or numeric product ID.
- Create a Shopify cart from selected variant IDs and quantities, returning a checkout URL.
- Flatten GraphQL `edges`/`node` wrappers into simpler JSON.
- Health endpoint, request logging, permissive CORS, and graceful shutdown.
- Multi-stage Docker image and Fly.io deployment configuration.

## Architecture

```text
Flutter client --HTTP/JSON--> Go server --GraphQL/HTTPS--> Shopify Storefront API
                                |
                      product response mapping
                      cart creation / checkout URL
```

The checkout is **not processed by the Go server**: Shopify returns a hosted checkout URL for the client to open.

## Requirements

- Go 1.23.3 or newer compatible Go version (module declares Go 1.23.3).
- A Shopify store with Storefront API access and a Storefront access token.
- Docker is optional.

## Local setup

1. Clone the repository and enter it.
2. Set the variables shown below in your shell or preferred secret manager.
3. Run `go run .`.
4. Check `http://localhost:8080/api/v1/health`.

Example (POSIX shell):

```sh
export SERVER_PORT=8080
export ENV=dev
export SHOPIFY_STORE_NAME=your-store-subdomain
export SHOPIFY_STOREFRONT_TOKEN=your-storefront-token
go run .
curl http://localhost:8080/api/v1/health
```

`SHOPIFY_STORE_NAME` is the subdomain only, without `.myshopify.com`. The server constructs the Shopify URL itself.

**Important:** Although `.env` is in `.gitignore`, the application does not load a `.env` file automatically. Export variables in your shell or use an environment loader. Never commit real credentials.

### Configuration

| Variable | Default in code | Purpose |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | HTTP listen port |
| `ENV` | `dev` | Adds a development query parameter to the checkout URL when `dev` |
| `SHOPIFY_STORE_NAME` | `test-store` | Shopify store subdomain |
| `SHOPIFY_STOREFRONT_TOKEN` | placeholder | Storefront GraphQL token |
| `SHOPIFY_ADMIN_TOKEN` | placeholder | Configured but not currently needed by registered REST routes |
| `SHOPIFY_API_VERSION` | `2026-07` | **Currently not honored in endpoint construction**; see roadmap |

The source currently hardcodes `2026-07` as the Shopify API version when constructing both GraphQL endpoints. Do not assume changing `SHOPIFY_API_VERSION` changes the requested API version.

### Docker

```sh
docker build -t ecommerce-proxy .
docker run --rm -p 8080:8080 \
  -e SERVER_PORT=8080 \
  -e SHOPIFY_STORE_NAME=your-store-subdomain \
  -e SHOPIFY_STOREFRONT_TOKEN=your-storefront-token \
  ecommerce-proxy
```

The included `fly.toml` targets Fly.io and its internal port 8080. Supply tokens as deployment secrets; do not put secrets in `fly.toml`.

## HTTP API

| Method | Route | Purpose |
| --- | --- | --- |
| GET | `/api/v1/health` | Simple health response |
| GET | `/api/v1/products?limit=10` | List products |
| GET | `/api/v1/product/{product_id}` | Get a product by Shopify ID |
| POST | `/api/v1/checkout` | Create a Shopify cart and return its checkout URL |

See [API reference](docs/API.md) for examples and current behavior.

## Project layout

| File | Responsibility |
| --- | --- |
| `main.go` | Process lifecycle and graceful shutdown |
| `server.go` | Environment configuration, HTTP server, route registration |
| `api.go` | REST handlers and response encoding |
| `shopify.go` | Storefront GraphQL queries, cart mutation, HTTP requests |
| `types.go` | Configuration and JSON/GraphQL models |
| `utils.go` | Middleware, ID normalization and GraphQL response transforms |
| `Dockerfile` / `fly.toml` | Container build and deployment configuration |

For implementation details see [architecture](docs/ARCHITECTURE.md). For known limitations and prioritized work see [roadmap](docs/ROADMAP.md).

## Development branches

**This README and the API/architecture references document the current `main` branch**, not every work-in-progress branch. As inspected on 2026-11-14, `dev` and `product-caching` point to the same commit (`21c5416`) and include checkout-session tracking, Shopify webhooks, Server-Sent Events (SSE), and session cleanup. Those endpoints/features are **not on `main`**. No implemented product cache or rate-limit middleware was found in the pushed development tree.

See [development branch review](docs/DEVELOPMENT_BRANCHES.md) for the branch-specific HTTP API and cautions. In particular, the SSE development code schedules a fake order event after every checkout and should not be considered production-ready.

## Related client

A separate Flutter application consumes this API. Keeping the client and backend in separate repositories is reasonable because they have independent build/deployment workflows; link the Flutter repo here once it is published. Do not include employer-owned material unless publication is authorized.

## Security and maturity notes

This repository is a prototype, not a security-reviewed production service. It currently permits all CORS origins, does not authenticate callers, has weak error handling and input validation, and creates a new HTTP client per Shopify request without an explicit timeout. Review [ROADMAP.md](docs/ROADMAP.md) before exposing the service broadly.
