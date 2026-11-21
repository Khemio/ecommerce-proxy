package main

import (
    "bytes"
    "crypto/hmac"
    "crypto/sha256"
    "encoding/base64"
    "io"
    "log"
    "net/http"
    "net/http/httptest"
    "testing"
)

func testMarketplaceServer() *MarketplaceServer {
    return &MarketplaceServer{
        config: Config{ShopifySecretToken: "test-webhook-secret"},
        logger: log.New(io.Discard, "", 0),
        sessions: make(map[string]*Session),
    }
}

func signWebhook(body []byte, secret string) string {
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write(body)
    return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestCheckoutRejectsInvalidInput(t *testing.T) {
    tests := []struct {
        name string
        body string
    }{
        {"empty cart", `[]`},
        {"missing variant", `[{"quantity":1}]`},
        {"zero quantity", `[{"variantId":"123","quantity":0}]`},
        {"negative quantity", `[{"variantId":"123","quantity":-1}]`},
        {"malformed JSON", `[{`},
        {"multiple JSON values", `[{"variantId":"123","quantity":1}] {}`},
        {"unknown field", `[{"variantId":"123","quantity":1,"other":true}]`},
    }
    server := testMarketplaceServer()
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            req := httptest.NewRequest(http.MethodPost, "/api/v1/checkout", bytes.NewBufferString(tt.body))
            resp := httptest.NewRecorder()
            server.HandleCheckout(resp, req)
            if resp.Code != http.StatusBadRequest {
                t.Fatalf("expected HTTP 400, got %d: %s", resp.Code, resp.Body.String())
            }
        })
    }
}

func TestWebhookRejectsInvalidSignatureAndJSON(t *testing.T) {
    server := testMarketplaceServer()
    testCases := []struct {
        name   string
        body   string
        signed bool
        want   int
    }{
        {"bad signature", `{"id":1}`, false, http.StatusUnauthorized},
        {"malformed signed payload", `{`, true, http.StatusBadRequest},
        {"valid signed unrelated order", `{"id":1}`, true, http.StatusOK},
    }
    for _, tt := range testCases {
        t.Run(tt.name, func(t *testing.T) {
            body := []byte(tt.body)
            req := httptest.NewRequest(http.MethodPost, "/api/v1/shopify/webhook", bytes.NewReader(body))
            signature := "not-a-valid-hmac"
            if tt.signed { signature = signWebhook(body, server.config.ShopifySecretToken) }
            req.Header.Set("X-Shopify-Hmac-Sha256", signature)
            resp := httptest.NewRecorder()
            server.HandleShopifyWebhook(resp, req)
            if resp.Code != tt.want {
                t.Fatalf("expected HTTP %d, got %d: %s", tt.want, resp.Code, resp.Body.String())
            }
        })
    }
}
