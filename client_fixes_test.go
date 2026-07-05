package bdds

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newAuthServer returns a mock OAuth2 token endpoint that issues tokens with the
// given expires_in. Each issued token is unique so callers can assert refreshes.
func newAuthServer(expiresIn int) (*httptest.Server, *int32) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "token-" + time.Now().Format("150405.000000000") + "-" + string(rune('0'+n%10)),
			"token_type":   "Bearer",
			"expires_in":   expiresIn,
			"scope":        "openid",
		})
	}))
	return srv, &calls
}

// newTestClient wires a client to the given API server, redirecting auth to the
// provided auth server.
func newTestClient(t *testing.T, apiURL, authURL string) *Client {
	t.Helper()
	client, err := NewClient(&Config{
		Username:   "u",
		Password:   "p",
		BaseURL:    apiURL,
		MaxRetries: 3,
		RetryDelay: time.Millisecond,
		Timeout:    5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.httpClient = &http.Client{
		Transport: &testTransport{
			authURL: authURL + "/oauth2/aus3up3nz0N133c0V417/v1/token",
			rt:      http.DefaultTransport,
		},
		Timeout: 5 * time.Second,
	}
	return client
}

// TestConcurrentTokenAccess exercises ensureValidToken/clearToken from many
// goroutines to confirm the token state is race-free (run under -race).
func TestConcurrentTokenAccess(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "x", "description": "y"}})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := client.ListProducts(ctx); err != nil {
				t.Errorf("ListProducts: %v", err)
			}
			client.clearToken()
		}()
	}
	wg.Wait()
}

// TestDead401Retry verifies that an API-level 401 clears the token and triggers
// re-authentication, eventually succeeding.
func TestDead401Retry(t *testing.T) {
	authServer, authCalls := newAuthServer(3600)
	defer authServer.Close()

	var apiCalls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// First call returns 401, subsequent calls succeed.
		if atomic.AddInt32(&apiCalls, 1) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"expired"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "x", "description": "y"}})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	products, err := client.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("ListProducts after 401 retry: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	// Auth should have happened at least twice: initial + after clearing on 401.
	if c := atomic.LoadInt32(authCalls); c < 2 {
		t.Errorf("expected re-authentication after 401, auth calls = %d", c)
	}
}

// TestExpiresInHonored confirms a short expires_in shortens the cached TTL.
func TestExpiresInHonored(t *testing.T) {
	// expires_in below the refresh buffer means the token is always "due".
	authServer, authCalls := newAuthServer(60)
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "x", "description": "y"}})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)
	ctx := context.Background()

	if _, err := client.ListProducts(ctx); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := client.ListProducts(ctx); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// 60s < 5m refresh buffer, so each request must re-authenticate.
	if c := atomic.LoadInt32(authCalls); c < 2 {
		t.Errorf("expected token to be re-fetched (expires_in honored), auth calls = %d", c)
	}

	// A long expires_in should be cached and reused across requests.
	authServer2, authCalls2 := newAuthServer(3600)
	defer authServer2.Close()
	client2 := newTestClient(t, apiServer.URL, authServer2.URL)
	if _, err := client2.ListProducts(ctx); err != nil {
		t.Fatalf("client2 first call: %v", err)
	}
	if _, err := client2.ListProducts(ctx); err != nil {
		t.Fatalf("client2 second call: %v", err)
	}
	if c := atomic.LoadInt32(authCalls2); c != 1 {
		t.Errorf("expected single auth with long TTL, got %d", c)
	}
}

// TestNewClientDoesNotMutateConfig verifies NewClient copies the caller config.
func TestNewClientDoesNotMutateConfig(t *testing.T) {
	cfg := &Config{Username: "u", Password: "p"}
	if _, err := NewClient(cfg); err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if cfg.Timeout != 0 || cfg.BaseURL != "" || cfg.MaxRetries != 0 || cfg.RetryDelay != 0 {
		t.Errorf("NewClient mutated caller config: %+v", cfg)
	}
}

// TestRetryContextCancellation ensures the backoff wait honors context cancel.
func TestRetryContextCancellation(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)
	client.config.RetryDelay = time.Hour // make the wait long enough to cancel

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := client.ListProducts(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled retry")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("retry did not honor context cancellation, took %s", elapsed)
	}
}

// TestNonRetryableNotRetried verifies that a 4xx (non-401) is returned without
// retrying.
func TestNonRetryableNotRetried(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	var apiCalls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad"}`))
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	_, err := client.ListProducts(context.Background())
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if c := atomic.LoadInt32(&apiCalls); c != 1 {
		t.Errorf("expected exactly 1 API call (no retry on 400), got %d", c)
	}
}

// TestServerErrorRetried verifies that 5xx responses are retried.
func TestServerErrorRetried(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	var apiCalls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&apiCalls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "x", "description": "y"}})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	products, err := client.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("expected success after 5xx retries: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if c := atomic.LoadInt32(&apiCalls); c != 3 {
		t.Errorf("expected 3 API calls (2 failures + success), got %d", c)
	}
}

// TestRateLimitHandling verifies 429 maps to RateLimitError, honors Retry-After,
// and is retried.
func TestRateLimitHandling(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	var apiCalls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if atomic.AddInt32(&apiCalls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]interface{}{{"id": 1, "name": "x", "description": "y"}})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	products, err := client.ListProducts(context.Background())
	if err != nil {
		t.Fatalf("expected success after 429 retry: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
}

// TestRateLimitErrorTyped verifies a non-retryable 429 (retries exhausted)
// surfaces as *RateLimitError with the Retry-After value.
func TestRateLimitErrorTyped(t *testing.T) {
	err := statusToError(http.StatusTooManyRequests, http.Header{"Retry-After": []string{"42"}}, []byte("slow down"))
	var rl *RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("expected *RateLimitError, got %T", err)
	}
	if rl.RetryAfter != 42 {
		t.Errorf("expected RetryAfter 42, got %d", rl.RetryAfter)
	}
}

// TestStatusToError401 verifies 401 maps to *AuthError.
func TestStatusToError401(t *testing.T) {
	err := statusToError(http.StatusUnauthorized, nil, []byte("nope"))
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AuthError, got %T", err)
	}
	if ae.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", ae.StatusCode)
	}
}

// TestGetLatestDeliverySkipsNotifications verifies notification/admin deliveries
// are not chosen as the latest data delivery.
func TestGetLatestDeliverySkipsNotifications(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": 1, "name": "x", "description": "y",
			"deliveries": []map[string]interface{}{
				{
					"deliveryId":                  1,
					"deliveryName":                "2026-06-01",
					"deliveryPublicationDatetime": "2026-06-01T10:00:00Z",
					"deliveryExpiryDatetime":      nil,
					"files":                       []map[string]interface{}{},
				},
				{
					"deliveryId":                  2,
					"deliveryName":                "NOTIFICATION: NEW DTD",
					"deliveryPublicationDatetime": "2026-06-03T10:00:00Z",
					"deliveryExpiryDatetime":      nil,
					"files":                       []map[string]interface{}{},
				},
			},
		})
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	delivery, err := client.GetLatestDelivery(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetLatestDelivery: %v", err)
	}
	if delivery.DeliveryID != 1 {
		t.Errorf("expected data delivery 1 (not the newer notification), got %d", delivery.DeliveryID)
	}
}

// newPartialDownloadServer returns an API server whose first failCount download
// responses announce the full Content-Length but drop the connection after
// sending only the first partial bytes, then serve the complete content.
func newPartialDownloadServer(t *testing.T, content []byte, partial, failCount int) (*httptest.Server, *int32) {
	t.Helper()
	var apiCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if int(atomic.AddInt32(&apiCalls, 1)) <= failCount {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("response writer does not support hijacking")
				return
			}
			conn, buf, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			// Announce the full length, write a partial body, then close so
			// the client sees an unexpected EOF mid-copy.
			_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(content)) + "\r\n\r\n")
			_, _ = buf.Write(content[:partial])
			_ = buf.Flush()
			_ = conn.Close()
			return
		}
		_, _ = w.Write(content)
	}))
	return srv, &apiCalls
}

// TestDownloadRetryByteExact verifies that a download retried after a partial
// write rewinds a seekable destination, producing byte-exact output.
func TestDownloadRetryByteExact(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	content := []byte("full delivery file content that must arrive byte-exact after a retry")
	apiServer, apiCalls := newPartialDownloadServer(t, content, 10, 1)
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	tmp, err := os.CreateTemp(t.TempDir(), "bdds-download-*.zip")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer func() { _ = tmp.Close() }()

	if err := client.DownloadFile(context.Background(), 1, 2, 3, tmp); err != nil {
		t.Fatalf("DownloadFile after partial-write retry: %v", err)
	}
	got, err := os.ReadFile(tmp.Name())
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("downloaded content corrupted by retry:\n got %q\nwant %q", got, content)
	}
	if c := atomic.LoadInt32(apiCalls); c != 2 {
		t.Errorf("expected 2 download attempts (partial + success), got %d", c)
	}
}

// nonSeekableWriter hides any Seek method, standing in for a destination (e.g.
// a network stream) that cannot be rewound between retries.
type nonSeekableWriter struct{ buf bytes.Buffer }

func (w *nonSeekableWriter) Write(p []byte) (int, error) { return w.buf.Write(p) }

// TestDownloadPartialWriteNonSeekableFailsFast verifies that a partial write to
// a non-seekable destination fails immediately instead of retrying and
// appending corrupted output.
func TestDownloadPartialWriteNonSeekableFailsFast(t *testing.T) {
	authServer, _ := newAuthServer(3600)
	defer authServer.Close()

	content := []byte("full delivery file content that never completes")
	apiServer, apiCalls := newPartialDownloadServer(t, content, 10, 100)
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	dst := &nonSeekableWriter{}
	err := client.DownloadFile(context.Background(), 1, 2, 3, dst)
	if err == nil {
		t.Fatal("expected error for interrupted download to non-seekable destination")
	}
	if c := atomic.LoadInt32(apiCalls); c != 1 {
		t.Errorf("expected exactly 1 download attempt (no retry after partial write), got %d", c)
	}
	if dst.buf.Len() != 10 {
		t.Errorf("expected only the 10 partial bytes in destination, got %d", dst.buf.Len())
	}
}

// TestAlways401SingleReauth verifies a persistent 401 triggers exactly one
// re-authentication before surfacing the typed auth error.
func TestAlways401SingleReauth(t *testing.T) {
	authServer, authCalls := newAuthServer(3600)
	defer authServer.Close()

	var apiCalls int32
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&apiCalls, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid subscription"}`))
	}))
	defer apiServer.Close()

	client := newTestClient(t, apiServer.URL, authServer.URL)

	_, err := client.ListProducts(context.Background())
	if err == nil {
		t.Fatal("expected error for persistent 401")
	}
	var ae *AuthError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *AuthError, got %T: %v", err, err)
	}
	if ae.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", ae.StatusCode)
	}
	if c := atomic.LoadInt32(&apiCalls); c != 2 {
		t.Errorf("expected exactly 2 API calls (initial + single re-auth retry), got %d", c)
	}
	if c := atomic.LoadInt32(authCalls); c != 2 {
		t.Errorf("expected exactly 2 auth calls (initial + single re-auth), got %d", c)
	}
}

// TestNonRetryableErrorStopsRetry verifies the nonRetryableError sentinel is
// never retried and keeps the wrapped error reachable via errors.Is.
func TestNonRetryableErrorStopsRetry(t *testing.T) {
	client := &Client{config: DefaultConfig()}
	inner := errors.New("permanent failure")
	wrapped := &nonRetryableError{err: inner}

	if retry, _ := client.classifyRetry(wrapped); retry {
		t.Error("expected nonRetryableError to be classified as not retryable")
	}
	if !errors.Is(wrapped, inner) {
		t.Error("expected wrapped error to be reachable via errors.Is")
	}

	// A plain unexpected error stays retryable, so the sentinel is what stops
	// the loop, not a broader behavior change.
	if retry, _ := client.classifyRetry(inner); !retry {
		t.Error("expected plain error to remain retryable")
	}
}
