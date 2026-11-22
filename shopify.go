package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HttpHeader struct {
	Key   string
	Value string
}

func FetchProduct(ctx context.Context, config *Config, id string) (GraphQLResponse[ProductQueryResult], error) {
	// You can use `product(handle:)` to query a single product by its handle instead.
	query := `
		query getProduct($id: ID!) {
			product(id: $id) {
				title
				description
				productType
				variants(first: 1) {
					edges {
						node {
							price {
								amount
							}
							sku
							id
						}
					}
				}
				images(first: 1) {
					edges {
						node {
							url
						}
					}
				}
			}
		}`

	productId := id

	// Double check: Ensure it starts with "gid://"
	if !strings.HasPrefix(productId, "gid://") {
		productId = fmt.Sprintf("gid://shopify/Product/%s", id)
	}

	variables := map[string]any{
		"id": productId,
	}

	body, err := sendStorefrontRequest(ctx, config, query, variables)
	if err != nil {
		return GraphQLResponse[ProductQueryResult]{}, err
	}

	return parseGraphQL[ProductQueryResult](body)
}

func FetchProducts(ctx context.Context, config *Config, limit int) (GraphQLResponse[QueryResult], error) {

	query := `
			query getProducts($limit: Int!) {
				products(first: $limit) {
					edges {
						node {
							id
							title
							description
							productType
							vendor
							availableForSale
							variants(first: 1) {
								edges {
									node {
										price {
											amount
										}
										sku
										id
									}
								}
							}
							images(first: 1) {
								edges {
									node {
										url
									}
								}
							}
						}
					}
				}
			}`

	variables := map[string]any{
		"limit": limit,
	}

	// body, err := sendAdminRequest(config, query, variables)
	body, err := sendStorefrontRequest(ctx, config, query, variables)
	if err != nil {
		return GraphQLResponse[QueryResult]{}, err
	}

	return parseGraphQL[QueryResult](body)
}

func CreateCartAndCheckout(
	ctx context.Context,
	config *Config,
	items []Item,
	sessionID string,
) (GraphQLResponse[CheckoutQueryResult], error) {

	query := `
    mutation cartCreate($input: CartInput!) {
        cartCreate(input: $input) {
            cart {
                id
                checkoutUrl
                totalQuantity
				attributes {
					key
					value
				}
            }
            userErrors {
                field
                message
            }
        }
    }`

	// Define Variables
	var lines []map[string]any

	for _, item := range items {
		lines = append(lines, map[string]any{
			"merchandiseId": NormalizeMerchendiseId(item.VariantID),
			"quantity":      item.Quantity,
		})
	}

	variables := map[string]any{
		"input": map[string]any{
			"lines": lines,

			"attributes": []map[string]string{
				{
					"key":   "go_session_id",
					"value": sessionID,
				},
			},
		},
	}

	// Execute Storefront Request
	body, err := sendStorefrontRequest(ctx, config, query, variables)
	if err != nil {
		return GraphQLResponse[CheckoutQueryResult]{}, err
	}

	graphQLResp, err := parseGraphQL[CheckoutQueryResult](body)

	if err != nil {
		return graphQLResp, err
	}

	// TODO check that url is not empty
	if config.Environment == "dev" {
		originalUrl := graphQLResp.Data.CartCreate.Cart.CheckoutUrl
		separator := "?"
		if strings.Contains(originalUrl, "?") {
			separator = "&"
		}
		graphQLResp.Data.CartCreate.Cart.CheckoutUrl = originalUrl + separator + "channel=headless-storefronts"
	}

	return graphQLResp, nil
}

func sendAdminRequest(ctx context.Context, config *Config, query string, variables map[string]any) ([]byte, error) {
	accessHeader := HttpHeader{
		Key:   "X-Shopify-Access-Token",
		Value: config.ShopifyAdminToken,
	}
	res, err := sendGraphQLRequest(ctx, config.AdminEndpoint, accessHeader, query, variables)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func sendStorefrontRequest(ctx context.Context, config *Config, query string, variables map[string]any) ([]byte, error) {
	accessHeader := HttpHeader{
		Key:   "X-Shopify-Storefront-Access-Token",
		Value: config.ShopifyStorefrontToken,
	}
	res, err := sendGraphQLRequest(ctx, config.StorefrontEndpoint, accessHeader, query, variables)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func sendGraphQLRequest(
	ctx context.Context,
	endpoint string,
	accessHeader HttpHeader,
	query string,
	variables map[string]any,
) ([]byte, error) {
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode GraphQL request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, endpoint, bytes.NewReader(data),
	)
	if err != nil {
		return nil, fmt.Errorf("create Shopify request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(accessHeader.Key, accessHeader.Value)

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send Shopify request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Shopify returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read Shopify response: %w", err)
	}

	return body, nil
}

func parseGraphQL[T any](body []byte) (GraphQLResponse[T], error) {
	var result GraphQLResponse[T]

	if err := json.Unmarshal(body, &result); err != nil {
		return result, fmt.Errorf("decode Shopify response: %w", err)
	}

	if len(result.Errors) > 0 {
		return result, fmt.Errorf(
			"Shopify GraphQL error: %s",
			result.Errors[0].Message,
		)
	}

	return result, nil
}
