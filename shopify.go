package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type HttpHeader struct {
	Key   string
	Value string
}

func FetchProduct(config *Config, id string) GraphQLResponse[ProductQueryResult] {
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

	body, err := sendStorefrontRequest(config, query, variables)
	if err != nil {
		fmt.Println("Something went wrong with GraphQL request")
	}

	var graphQLResp GraphQLResponse[ProductQueryResult]

	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		fmt.Println("JSON Error:", err)
		// return
	}

	if len(graphQLResp.Errors) > 0 {
		fmt.Println("GraphQL Errors:", graphQLResp.Errors)
		// return
	}

	return graphQLResp
}

// TODO: handle errors
func FetchProducts(config *Config, limit int) GraphQLResponse[QueryResult] {

	query := `
			query getProducts($limit: Int!) {
				products(first: $limit) {
					edges {
						node {
							id
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
					}
				}
			}`

	variables := map[string]any{
		"limit": limit,
	}

	// body, err := sendAdminRequest(config, query, variables)
	body, err := sendStorefrontRequest(config, query, variables)
	if err != nil {
		fmt.Println("Something went wrong with GraphQL request")
	}

	var graphQLResp GraphQLResponse[QueryResult]

	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		fmt.Println("JSON Error:", err)
		// return
	}

	if len(graphQLResp.Errors) > 0 {
		fmt.Println("GraphQL Errors:", graphQLResp.Errors)
		// return
	}

	return graphQLResp
}

// POST /api/cart
// TODO: Handle errors
func CreateCartAndCheckout(config *Config, items []Item) GraphQLResponse[CheckoutQueryResult] {
	// Parse input from mobile app
	// 	[
	// 	{
	// 	  "variantId": "gid://shopify/ProductVariant/49042404180185",
	// 	  "quantity": 1
	// 	},
	// 	{
	// 	  "variantId": "gid://shopify/ProductVariant/49042403721433",
	// 	  "quantity": 1
	// 	}
	// ]

	query := `
    mutation cartCreate($input: CartInput!) {
        cartCreate(input: $input) {
            cart {
                id
                checkoutUrl
                totalQuantity
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
		},
	}

	// Execute Storefront Request
	body, err := sendStorefrontRequest(config, query, variables)
	if err != nil {
		// http.Error(w, err.Error(), http.StatusInternalServerError)
		// return
	}

	var graphQLResp GraphQLResponse[CheckoutQueryResult]

	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		fmt.Println("JSON Error:", err)
		// return
	}

	if len(graphQLResp.Errors) > 0 {
		fmt.Println("GraphQL Errors:", graphQLResp.Errors)
		// return
	}

	if config.Environment == "dev" {
		originalUrl := graphQLResp.Data.CartCreate.Cart.CheckoutUrl
		separator := "?"
		if strings.Contains(originalUrl, "?") {
			separator = "&"
		}
		graphQLResp.Data.CartCreate.Cart.CheckoutUrl = originalUrl + separator + "channel=headless-storefronts"
	}

	return graphQLResp
}

func sendAdminRequest(config *Config, query string, variables map[string]any) ([]byte, error) {
	accessHeader := HttpHeader{
		Key:   "X-Shopify-Access-Token",
		Value: config.ShopifyAdminToken,
	}
	res, err := sendGraphQLRequest(config.AdminEndpoint, accessHeader, query, variables)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func sendStorefrontRequest(config *Config, query string, variables map[string]any) ([]byte, error) {
	accessHeader := HttpHeader{
		Key:   "X-Shopify-Storefront-Access-Token",
		Value: config.ShopifyStorefrontToken,
	}
	res, err := sendGraphQLRequest(config.StorefrontEndpoint, accessHeader, query, variables)
	if err != nil {
		return nil, err
	}

	return res, nil
}

func sendGraphQLRequest(endpoint string, accessHeader HttpHeader, query string, variables map[string]any) ([]byte, error) {

	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(accessHeader.Key, accessHeader.Value)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}
