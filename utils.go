package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func NormalizeMerchendiseId(id string) string {
	// Format the Global ID required by Storefront API
	merchandiseId := id

	// Double check: Ensure it starts with "gid://"
	if !strings.HasPrefix(merchandiseId, "gid://") {
		// Fallback: Format it if it's just a number
		merchandiseId = fmt.Sprintf("gid://shopify/ProductVariant/%s", id)
	}

	return merchandiseId
}

// Middleware factory for universal payload logging and request performance metrics
func LoggingMiddleware(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			next.ServeHTTP(w, r)
			logger.Printf("%s -- %s %s took %v", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start))
		})
	}
}

// CorsMiddleware injects necessary headers allowing Flutter Chrome to safely read responses
func CorsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allows requests from your Flutter Web local dev environment
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS, PUT, DELETE")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight OPTIONS requests sent automatically by Chrome
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GetEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func FlattenGraphQLResponse(input map[string]any) map[string]any {
	flattened := make(map[string]any)
	flattenRecursive(input, "", flattened)
	return flattened
}

func flattenRecursive(current map[string]any, prefix string, dest map[string]any) {
	for key, value := range current {
		// Construct the full key for the destination map
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		// Check if the value is a slice (common for GraphQL edges/nodes)
		if sliceVal, ok := value.([]any); ok {
			// If the slice contains maps (e.g., edges -> node), flatten each item
			// and merge them into the destination at the current level or a new key
			for _, item := range sliceVal {
				if itemMap, ok := item.(map[string]any); ok {
					// If we are inside a known wrapper like "edges" or "nodes",
					// we might want to flatten the inner map directly into the parent
					// or keep the index. Here we flatten the inner map into dest.
					flattenRecursive(itemMap, fullKey, dest)
				} else {
					dest[fullKey] = item
				}
			}
			continue
		}

		// Check if the value is a map (nested structure)
		if nestedMap, ok := value.(map[string]any); ok {
			// Heuristic: If the key is a common wrapper, skip it and flatten its contents
			// into the current scope (or parent scope).
			isWrapper := key == "edges" || key == "nodes" || key == "data" || key == "results" || key == "items"

			if isWrapper {
				// Flatten the wrapper's contents into the parent destination
				// using the parent prefix (strip the wrapper key itself)
				flattenRecursive(nestedMap, prefix, dest)
			} else {
				// Standard nested map: recurse with updated prefix
				flattenRecursive(nestedMap, fullKey, dest)
			}
			continue
		}

		// Primitive value: store it
		dest[fullKey] = value
	}
}

// TransformGraphQLResponse removes 'edges' and 'node' wrappers.
// - 'edges' arrays are converted to direct arrays of objects.
// - 'node' keys are removed, leaving their object content in place.
func TransformGraphQLResponse(input any) any {
	switch v := input.(type) {
	case map[string]any:
		// 1. Handle 'edges' wrapper: convert to array of nodes
		if edgesVal, ok := v["edges"]; ok {
			if edges, ok := edgesVal.([]any); ok {
				var nodes []any
				for _, edge := range edges {
					if edgeMap, ok := edge.(map[string]any); ok {
						if nodeVal, ok := edgeMap["node"]; ok {
							// Recurse into node content
							nodes = append(nodes, TransformGraphQLResponse(nodeVal))
						} else {
							// Fallback if 'node' key is missing
							nodes = append(nodes, TransformGraphQLResponse(edgeMap))
						}
					} else {
						nodes = append(nodes, edge)
					}
				}
				return nodes
			}
		}

		// 2. Handle 'node' wrapper: lift content up
		// If the map ONLY contains 'node' (or if you want to lift 'node' regardless of siblings),
		// you can handle it here.
		// Based on your request ("top level 'node' remains"), if a map has a key 'node'
		// and you want to discard the key but keep the object:
		if nodeVal, ok := v["node"]; ok {
			// If the value of 'node' is a map, return its transformed content directly
			if nodeMap, ok := nodeVal.(map[string]any); ok {
				return TransformGraphQLResponse(nodeMap)
			}
			// If 'node' is not a map (rare), just return the transformed value
			return TransformGraphQLResponse(nodeVal)
		}

		// 3. Standard map: recurse into values
		result := make(map[string]any)
		for k, val := range v {
			result[k] = TransformGraphQLResponse(val)
		}
		return result

	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = TransformGraphQLResponse(item)
		}
		return result

	default:
		return v
	}
}
