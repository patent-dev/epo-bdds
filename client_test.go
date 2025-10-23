package bdds

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDefaultConfig tests the DefaultConfig function
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if config.BaseURL != "https://publication-bdds.apps.epo.org" {
		t.Errorf("Expected BaseURL to be https://publication-bdds.apps.epo.org, got %s", config.BaseURL)
	}
	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries to be 3, got %d", config.MaxRetries)
	}
	if config.RetryDelay != 1 {
		t.Errorf("Expected RetryDelay to be 1, got %d", config.RetryDelay)
	}
	if config.Timeout != 30 {
		t.Errorf("Expected Timeout to be 30, got %d", config.Timeout)
	}
}

// TestNewClient_WithoutCredentials tests that NewClient works without credentials
func TestNewClient_WithoutCredentials(t *testing.T) {
	config := &Config{}
	client, err := NewClient(config)
	if err != nil {
		t.Errorf("Expected no error when creating client without credentials, got: %v", err)
	}
	if client == nil {
		t.Error("Expected client to be created")
	}
	// Client should work for downloads even without credentials
}

// TestClientWithMockServer tests all client methods with mock server
func TestClientWithMockServer(t *testing.T) {
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/aus3up3nz0N133c0V417/v1/token" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"access_token": "test-token-12345",
				"token_type":   "Bearer",
				"expires_in":   3600,
				"scope":        "openid",
				"id_token":     "test-id-token",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check auth header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")

		switch {
		case r.URL.Path == "/bdds/bdds-bff-service/prod/api/products/":
			// List products response
			json.NewEncoder(w).Encode([]map[string]interface{}{
				{
					"id":          3,
					"name":        "EP DocDB front file",
					"description": "EP DocDB front file - bibliographic data",
				},
				{
					"id":          4,
					"name":        "EP full-text data - front file",
					"description": "EP full-text data - front file",
				},
				{
					"id":          14,
					"name":        "EP DocDB back file",
					"description": "EP DocDB back file - bibliographic data",
				},
			})

		case strings.Contains(r.URL.Path, "/download"):
			// File download - check this before products/{id}
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", "17")
			w.Write([]byte("test-file-content"))

		case r.URL.Path == "/bdds/bdds-bff-service/prod/api/products/3":
			// Get product 3 with deliveries
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id":          3,
				"name":        "EP DocDB front file",
				"description": "EP DocDB front file - bibliographic data",
				"deliveries": []map[string]interface{}{
					{
						"deliveryId":                  12345,
						"deliveryName":                "2024-10-15",
						"deliveryPublicationDatetime": "2024-10-15T10:30:00Z",
						"deliveryExpiryDatetime":      "2024-11-15T10:30:00Z",
						"files": []map[string]interface{}{
							{
								"fileId":                  67890,
								"fileName":                "EP_docdb_20241015.zip",
								"fileSize":                "1.5 GB",
								"fileChecksum":            "a1b2c3d4e5f6",
								"filePublicationDatetime": "2024-10-15T10:30:00Z",
							},
						},
					},
					{
						"deliveryId":                  12340,
						"deliveryName":                "2024-10-14",
						"deliveryPublicationDatetime": "2024-10-14T10:30:00Z",
						"deliveryExpiryDatetime":      nil,
						"files": []map[string]interface{}{
							{
								"fileId":                  67880,
								"fileName":                "EP_docdb_20241014.zip",
								"fileSize":                "1.4 GB",
								"fileChecksum":            "b2c3d4e5f6a1",
								"filePublicationDatetime": "2024-10-14T10:30:00Z",
							},
						},
					},
				},
			})

		case r.URL.Path == "/bdds/bdds-bff-service/prod/api/products/999":
			// Product not found
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"product not found"}`))

		default:
			http.NotFound(w, r)
		}
	}))
	defer apiServer.Close()

	// Temporarily replace auth URL (not ideal but works for testing)
	// In real tests we'd use dependency injection
	config := &Config{
		Username:  "test-user",
		Password:  "test-pass",
		BaseURL:   apiServer.URL,
		UserAgent: "Test/1.0",
		Timeout:   10,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Override httpClient to use auth server (for testing)
	client.httpClient = &http.Client{
		Transport: &testTransport{
			authURL: authServer.URL + "/oauth2/aus3up3nz0N133c0V417/v1/token",
			rt:      http.DefaultTransport,
		},
		Timeout: 10 * time.Second,
	}

	ctx := context.Background()

	t.Run("ListProducts", func(t *testing.T) {
		products, err := client.ListProducts(ctx)
		if err != nil {
			t.Fatalf("ListProducts failed: %v", err)
		}
		if len(products) != 3 {
			t.Errorf("Expected 3 products, got %d", len(products))
		}
		if products[0].ID != 3 {
			t.Errorf("Expected first product ID to be 3, got %d", products[0].ID)
		}
		if products[0].Name != "EP DocDB front file" {
			t.Errorf("Expected product name 'EP DocDB front file', got %s", products[0].Name)
		}
	})

	t.Run("GetProduct", func(t *testing.T) {
		product, err := client.GetProduct(ctx, 3)
		if err != nil {
			t.Fatalf("GetProduct failed: %v", err)
		}
		if product.ID != 3 {
			t.Errorf("Expected product ID 3, got %d", product.ID)
		}
		if len(product.Deliveries) != 2 {
			t.Errorf("Expected 2 deliveries, got %d", len(product.Deliveries))
		}
		if product.Deliveries[0].DeliveryID != 12345 {
			t.Errorf("Expected delivery ID 12345, got %d", product.Deliveries[0].DeliveryID)
		}
		if len(product.Deliveries[0].Files) != 1 {
			t.Errorf("Expected 1 file, got %d", len(product.Deliveries[0].Files))
		}
		if product.Deliveries[0].Files[0].FileName != "EP_docdb_20241015.zip" {
			t.Errorf("Expected file name EP_docdb_20241015.zip, got %s", product.Deliveries[0].Files[0].FileName)
		}
	})

	t.Run("GetProduct_NotFound", func(t *testing.T) {
		_, err := client.GetProduct(ctx, 999)
		if err == nil {
			t.Error("Expected error for non-existent product")
		}
		// Check if error message contains "not found"
		if !strings.Contains(err.Error(), "not found") {
			t.Errorf("Expected error containing 'not found', got: %v", err)
		}
	})

	t.Run("GetProductByName", func(t *testing.T) {
		product, err := client.GetProductByName(ctx, "EP DocDB front file")
		if err != nil {
			t.Fatalf("GetProductByName failed: %v", err)
		}
		if product.ID != 3 {
			t.Errorf("Expected product ID 3, got %d", product.ID)
		}
	})

	t.Run("GetProductByName_NotFound", func(t *testing.T) {
		_, err := client.GetProductByName(ctx, "Non-existent Product")
		if err == nil {
			t.Error("Expected error for non-existent product name")
		}
		if _, ok := err.(*NotFoundError); !ok {
			t.Errorf("Expected NotFoundError, got %T: %v", err, err)
		}
	})

	t.Run("GetLatestDelivery", func(t *testing.T) {
		delivery, err := client.GetLatestDelivery(ctx, 3)
		if err != nil {
			t.Fatalf("GetLatestDelivery failed: %v", err)
		}
		// Should return the latest (2024-10-15)
		if delivery.DeliveryID != 12345 {
			t.Errorf("Expected latest delivery ID 12345, got %d", delivery.DeliveryID)
		}
		if delivery.DeliveryName != "2024-10-15" {
			t.Errorf("Expected latest delivery name 2024-10-15, got %s", delivery.DeliveryName)
		}
	})

	t.Run("DownloadFile", func(t *testing.T) {
		var buf bytes.Buffer
		err := client.DownloadFile(ctx, 3, 12345, 67890, &buf)
		if err != nil {
			t.Fatalf("DownloadFile failed: %v", err)
		}
		content := buf.String()
		if content != "test-file-content" {
			t.Errorf("Expected content 'test-file-content', got '%s'", content)
		}
	})

	t.Run("DownloadFileWithProgress", func(t *testing.T) {
		var buf bytes.Buffer
		progressCalled := false
		err := client.DownloadFileWithProgress(ctx, 3, 12345, 67890, &buf, func(current, total int64) {
			progressCalled = true
			if total != 17 {
				t.Errorf("Expected total bytes 17, got %d", total)
			}
		})
		if err != nil {
			t.Fatalf("DownloadFileWithProgress failed: %v", err)
		}
		if !progressCalled {
			t.Error("Progress callback was not called")
		}
	})
}

// testTransport helps redirect oauth requests to test server
type testTransport struct {
	authURL string
	rt      http.RoundTripper
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect auth requests to test auth server
	if strings.Contains(req.URL.String(), "login.epo.org") {
		req.URL.Host = strings.TrimPrefix(t.authURL, "http://")
		req.URL.Host = strings.TrimPrefix(req.URL.Host, "https://")
		req.URL.Scheme = "http"
		idx := strings.Index(t.authURL, "//")
		if idx >= 0 {
			hostPath := t.authURL[idx+2:]
			parts := strings.SplitN(hostPath, "/", 2)
			req.URL.Host = parts[0]
			if len(parts) > 1 {
				req.URL.Path = "/" + parts[1]
			}
		}
	}
	return t.rt.RoundTrip(req)
}

// TestTokenExpiry tests token expiry and refresh logic
func TestTokenExpiry(t *testing.T) {
	config := &Config{
		Username: "test",
		Password: "test",
	}
	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	// Initially no token
	if client.token != "" {
		t.Error("Expected no token initially")
	}

	// Set expired token
	client.token = "expired-token"
	client.tokenExpiry = time.Now().Add(-1 * time.Hour)

	// ensureValidToken should detect expiry
	// (will fail to refresh in this test, but that's expected)
	err = client.ensureValidToken(context.Background())
	if err == nil {
		t.Error("Expected error when refreshing with invalid credentials")
	}
}

// TestErrorTypes tests custom error types
func TestErrorTypes(t *testing.T) {
	t.Run("AuthError", func(t *testing.T) {
		err := &AuthError{StatusCode: 401, Message: "invalid credentials"}
		if err.Error() != "authentication failed (status 401): invalid credentials" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("NotFoundError", func(t *testing.T) {
		err := &NotFoundError{Resource: "product", ID: "123"}
		if err.Error() != "product not found: 123" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})

	t.Run("RateLimitError", func(t *testing.T) {
		err := &RateLimitError{RetryAfter: 60}
		if err.Error() != "rate limited, retry after 60 seconds" {
			t.Errorf("Unexpected error message: %s", err.Error())
		}
	})
}

// TestProgressReader tests the progress reader
func TestProgressReader(t *testing.T) {
	data := []byte("test content for progress tracking")
	reader := bytes.NewReader(data)

	var progressCalls int
	var lastCurrent, lastTotal int64

	pr := &progressReader{
		reader: reader,
		total:  int64(len(data)),
		progressFn: func(current, total int64) {
			progressCalls++
			lastCurrent = current
			lastTotal = total
		},
	}

	// Read all data
	buf, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if string(buf) != string(data) {
		t.Error("Data mismatch")
	}

	if progressCalls == 0 {
		t.Error("Progress function was not called")
	}

	if lastCurrent != int64(len(data)) {
		t.Errorf("Expected final current %d, got %d", len(data), lastCurrent)
	}

	if lastTotal != int64(len(data)) {
		t.Errorf("Expected final total %d, got %d", len(data), lastTotal)
	}
}
