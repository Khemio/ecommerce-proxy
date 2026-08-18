package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

func (s *MarketplaceServer) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"operational"}`))
}

func (s *MarketplaceServer) HandleFetchVendorProduct(w http.ResponseWriter, r *http.Request) {
	// Extract path parameter using the standard library method
	productId := r.PathValue("product_id")
	if productId == "" {
		http.Error(w, "Product ID required", http.StatusBadRequest)
		return
	}

	var graphQLResp GraphQLResponse[ProductQueryResult] = FetchProduct(&s.config, productId)
	product := TransformGraphQLResponse(graphQLResp.Data.Product).(map[string]any)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(product); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleFetchVendorProducts(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if i, err := strconv.Atoi(s); err == nil {
			limit = i
		}
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}

	var graphQLResp GraphQLResponse[QueryResult] = FetchProducts(&s.config, limit)

	for i := range graphQLResp.Data.Products.Edges {
		edge := &graphQLResp.Data.Products.Edges[i]
		*edge = TransformGraphQLResponse(*edge).(map[string]any)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(TransformGraphQLResponse(graphQLResp.Data.Products.Edges)); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}

func (s *MarketplaceServer) HandleCheckout(w http.ResponseWriter, r *http.Request) {
	var items []Item

	if err := json.NewDecoder(r.Body).Decode(&items); err != nil {
		http.Error(w, "Invalid input", http.StatusBadRequest)
		return
	}

	graphQLResp := CreateCartAndCheckout(&s.config, items)

	w.Header().Set("Content-Type", "application/json")
	// w.Write(body)
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(TransformGraphQLResponse(graphQLResp.Data.CartCreate.Cart)); err != nil {
		s.logger.Printf("Failed writing network buffer: %v", err)
	}
}
