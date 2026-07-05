// Package bdds provides a Go client for the European Patent Office's Bulk Data
// Distribution Service (BDDS), covering product discovery, delivery listing, and
// downloading of bulk patent datasets such as DOCDB, INPADOC, and EP full-text.
package bdds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/patent-dev/epo-bdds/generated"
)

// Version is the library version. It surfaces through the default User-Agent.
const Version = "0.2.2"

// DefaultUserAgent identifies this library in outbound requests.
const DefaultUserAgent = "epo-bdds-go/" + Version + " (patent.dev; +https://github.com/patent-dev/epo-bdds)"

const (
	// Default OAuth2 token TTL, used when the token response omits expires_in.
	defaultTokenTTL = time.Hour
	// Refresh token 5 minutes before expiry for safety
	tokenRefreshBuffer = 5 * time.Minute
)

// Client is the main EPO BDDS API client
type Client struct {
	config          *Config
	httpClient      *http.Client
	generatedClient *generated.ClientWithResponses

	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time
}

// Config holds client configuration
type Config struct {
	Username   string        // EPO BDDS username
	Password   string        // EPO BDDS password
	BaseURL    string        // Base URL for API (default: https://publication-bdds.apps.epo.org)
	UserAgent  string        // Optional custom user agent
	MaxRetries int           // Maximum number of retries (default: 3)
	RetryDelay time.Duration // Delay between retries (default: 1s)
	Timeout    time.Duration // Request timeout (default: 30s)
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BaseURL:    "https://publication-bdds.apps.epo.org",
		UserAgent:  DefaultUserAgent,
		MaxRetries: 3,
		RetryDelay: time.Second,
		Timeout:    30 * time.Second,
	}
}

// NewClient creates a new EPO BDDS API client
// Authentication is optional - free products work without credentials,
// paid products require EPO BDDS subscription and authentication.
func NewClient(config *Config) (*Client, error) {
	// Copy the caller's config so applying defaults never mutates their struct.
	cfg := DefaultConfig()
	if config != nil {
		*cfg = *config
	}

	// Apply defaults for any unset fields.
	defaults := DefaultConfig()
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaults.BaseURL
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaults.UserAgent
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = defaults.MaxRetries
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = defaults.RetryDelay
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaults.Timeout
	}
	config = cfg

	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	client := &Client{
		config:     config,
		httpClient: httpClient,
	}

	// Create generated client with request editor that adds auth
	genClient, err := generated.NewClientWithResponses(
		config.BaseURL+"/bdds/bdds-bff-service/prod/api",
		generated.WithHTTPClient(httpClient),
		generated.WithRequestEditorFn(client.authRequestEditor),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	client.generatedClient = genClient

	return client, nil
}

// authRequestEditor adds authentication and user agent to requests
func (c *Client) authRequestEditor(ctx context.Context, req *http.Request) error {
	// Skip authentication if no credentials provided
	if c.config.Username != "" && c.config.Password != "" {
		// Ensure we have a valid token
		token, err := c.ensureValidToken(ctx)
		if err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("User-Agent", c.config.UserAgent)
	return nil
}

// ensureValidToken returns a valid token, refreshing it if expired. All access
// to the cached token state is guarded by tokenMu so it is safe to call from
// concurrent requests.
func (c *Client) ensureValidToken(ctx context.Context) (string, error) {
	// Skip if no credentials configured
	if c.config.Username == "" || c.config.Password == "" {
		return "", nil
	}

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Check if token exists and is still valid
	if c.token != "" && time.Now().Add(tokenRefreshBuffer).Before(c.tokenExpiry) {
		return c.token, nil
	}

	// Need to authenticate or refresh
	if err := c.authenticateLocked(ctx); err != nil {
		return "", err
	}
	return c.token, nil
}

// clearToken invalidates the cached token so the next request re-authenticates.
func (c *Client) clearToken() {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	c.token = ""
	c.tokenExpiry = time.Time{}
}

// authenticateLocked performs OAuth2 password grant authentication. The caller
// must hold tokenMu.
func (c *Client) authenticateLocked(ctx context.Context) error {
	const (
		oauthURL = "https://login.epo.org/oauth2/aus3up3nz0N133c0V417/v1/token"
		clientID = "MG9hM3VwZG43YW41cE1JOE80MTc="
	)

	data := url.Values{
		"grant_type": {"password"},
		"username":   {c.config.Username},
		"password":   {c.config.Password},
		"scope":      {"openid"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", oauthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create auth request: %w", err)
	}

	req.Header.Set("Authorization", "Basic "+clientID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &AuthError{
			StatusCode: resp.StatusCode,
			Message:    string(body),
		}
	}

	var tokenResp generated.TokenResponse
	if err := readJSON(resp.Body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	ttl := defaultTokenTTL
	if tokenResp.ExpiresIn > 0 {
		ttl = time.Duration(tokenResp.ExpiresIn) * time.Second
	}

	c.token = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(ttl)

	return nil
}

// retryableRequest wraps requests with retry logic. It only retries transient
// failures (network errors, 5xx, 429) and 401s (after clearing the token to
// force re-authentication). Other 4xx responses are returned immediately. The
// backoff wait honours context cancellation.
func (c *Client) retryableRequest(ctx context.Context, fn func() error) error {
	var lastErr error
	reauthed := false
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		retry, after := c.classifyRetry(err)
		if !retry || attempt == c.config.MaxRetries {
			break
		}

		// On 401, force re-auth by clearing the cached token. Re-auth happens
		// at most once per call: a second 401 with a fresh token means the
		// credentials or subscription are rejected, which is permanent.
		var authErr *AuthError
		if errors.As(err, &authErr) && authErr.StatusCode == http.StatusUnauthorized {
			if reauthed {
				break
			}
			reauthed = true
			c.clearToken()
		}

		wait := time.Duration(attempt+1) * c.config.RetryDelay
		if after > wait {
			wait = after
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return fmt.Errorf("failed after %d retries: %w", c.config.MaxRetries, lastErr)
}

// classifyRetry reports whether err is transient and should be retried, plus an
// optional minimum wait (e.g. a Retry-After hint from a rate-limit response).
func (c *Client) classifyRetry(err error) (retry bool, after time.Duration) {
	var permanent *nonRetryableError
	if errors.As(err, &permanent) {
		return false, 0
	}

	var authErr *AuthError
	if errors.As(err, &authErr) {
		return authErr.StatusCode == http.StatusUnauthorized, 0
	}

	var rateErr *RateLimitError
	if errors.As(err, &rateErr) {
		return true, time.Duration(rateErr.RetryAfter) * time.Second
	}

	var statusErr *statusError
	if errors.As(err, &statusErr) {
		// Retry server errors; never retry other 4xx.
		return statusErr.StatusCode >= 500, 0
	}

	var notFound *NotFoundError
	if errors.As(err, &notFound) {
		return false, 0
	}

	// Network/transport errors and anything else unexpected: retry.
	return true, 0
}

// parseRetryAfter parses a Retry-After header value (delay seconds form only).
func parseRetryAfter(v string) int {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs > 0 {
		return secs
	}
	return 0
}

// statusToError maps a non-2xx HTTP status to a typed error: 401 -> *AuthError,
// 429 -> *RateLimitError (honouring Retry-After), everything else -> *statusError.
func statusToError(code int, header http.Header, body []byte) error {
	switch code {
	case http.StatusUnauthorized:
		return &AuthError{StatusCode: code, Message: string(body)}
	case http.StatusTooManyRequests:
		after := 0
		if header != nil {
			after = parseRetryAfter(header.Get("Retry-After"))
		}
		return &RateLimitError{RetryAfter: after}
	default:
		return &statusError{StatusCode: code, Body: string(body)}
	}
}

// ListProducts returns all available BDDS products
func (c *Client) ListProducts(ctx context.Context) ([]*Product, error) {
	var result []*Product
	err := c.retryableRequest(ctx, func() error {
		resp, err := c.generatedClient.ListProductsWithResponse(ctx)
		if err != nil {
			return err
		}

		if resp.StatusCode() != http.StatusOK {
			return statusToError(resp.StatusCode(), resp.HTTPResponse.Header, resp.Body)
		}

		if resp.JSON200 == nil {
			return fmt.Errorf("empty response body")
		}

		result = make([]*Product, len(*resp.JSON200))
		for i, p := range *resp.JSON200 {
			result[i] = &Product{
				ID:          p.Id,
				Name:        p.Name,
				Description: p.Description,
			}
		}
		return nil
	})

	return result, err
}

// GetProduct returns detailed information about a specific product including deliveries
func (c *Client) GetProduct(ctx context.Context, productID int) (*ProductWithDeliveries, error) {
	var result *ProductWithDeliveries
	err := c.retryableRequest(ctx, func() error {
		resp, err := c.generatedClient.GetProductWithResponse(ctx, productID)
		if err != nil {
			return err
		}

		if resp.StatusCode() == http.StatusNotFound {
			return &NotFoundError{
				Resource: "product",
				ID:       fmt.Sprintf("%d", productID),
			}
		}

		if resp.StatusCode() != http.StatusOK {
			return statusToError(resp.StatusCode(), resp.HTTPResponse.Header, resp.Body)
		}

		if resp.JSON200 == nil {
			return fmt.Errorf("empty response body")
		}

		p := resp.JSON200
		result = &ProductWithDeliveries{
			ID:          p.Id,
			Name:        p.Name,
			Description: p.Description,
			Deliveries:  make([]*Delivery, len(p.Deliveries)),
		}

		for i, d := range p.Deliveries {
			delivery := &Delivery{
				DeliveryID:                  d.DeliveryId,
				DeliveryName:                d.DeliveryName,
				DeliveryPublicationDatetime: d.DeliveryPublicationDatetime,
				DeliveryExpiryDatetime:      d.DeliveryExpiryDatetime,
				Files:                       make([]*DeliveryFile, len(d.Files)),
			}

			for j, f := range d.Files {
				delivery.Files[j] = &DeliveryFile{
					FileID:                  f.FileId,
					FileName:                f.FileName,
					FileSize:                f.FileSize,
					FileChecksum:            f.FileChecksum,
					FilePublicationDatetime: f.FilePublicationDatetime,
				}
			}

			result.Deliveries[i] = delivery
		}

		return nil
	})

	return result, err
}

// DownloadFile downloads a file to the provided writer
func (c *Client) DownloadFile(ctx context.Context, productID, deliveryID, fileID int, dst io.Writer) error {
	return c.DownloadFileWithProgress(ctx, productID, deliveryID, fileID, dst, nil)
}

// DownloadFileWithProgress downloads a file to the provided writer with progress callback.
// If an attempt fails after partially writing to dst, the retry rewinds a seekable
// destination (truncating it when supported, as *os.File is) before copying again,
// so the output is always byte-exact or the call errors - never silently corrupted.
// A non-seekable destination with partial data fails fast instead of retrying.
func (c *Client) DownloadFileWithProgress(ctx context.Context, productID, deliveryID, fileID int, dst io.Writer, progressFn func(bytesWritten, totalBytes int64)) error {
	counting := &countingWriter{w: dst}
	return c.retryableRequest(ctx, func() error {
		resp, err := c.generatedClient.DownloadFile(ctx, productID, deliveryID, fileID)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode == http.StatusNotFound {
			return &NotFoundError{
				Resource: "file",
				ID:       fmt.Sprintf("%d/%d/%d", productID, deliveryID, fileID),
			}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return statusToError(resp.StatusCode, resp.Header, body)
		}

		// A previous attempt already wrote bytes; rewind the destination so
		// the restarted copy cannot append to partial output.
		if counting.n > 0 {
			if err := restartDownloadDestination(dst); err != nil {
				return &nonRetryableError{err: err}
			}
			counting.n = 0
		}

		// If progress callback provided, wrap reader
		var reader io.Reader = resp.Body
		if progressFn != nil {
			reader = &progressReader{
				reader:     resp.Body,
				total:      resp.ContentLength,
				progressFn: progressFn,
			}
		}

		if _, err := io.Copy(counting, reader); err != nil {
			// Partial output in a destination that cannot be rewound would be
			// corrupted by a retry, so fail fast instead.
			if counting.n > 0 {
				if _, ok := dst.(io.Seeker); !ok {
					return &nonRetryableError{err: fmt.Errorf("download interrupted after %d bytes written to non-seekable destination, cannot retry safely: %w", counting.n, err)}
				}
			}
			return err
		}
		return nil
	})
}

// restartDownloadDestination rewinds a partially written download destination
// so a retried copy starts from the beginning. The writer must be seekable; if
// it also supports truncation (as *os.File does), the partial content is removed.
func restartDownloadDestination(dst io.Writer) error {
	seeker, ok := dst.(io.Seeker)
	if !ok {
		return fmt.Errorf("cannot retry download: destination is not seekable and already contains partial data")
	}
	if _, err := seeker.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to rewind destination for download retry: %w", err)
	}
	if truncator, ok := dst.(interface{ Truncate(int64) error }); ok {
		if err := truncator.Truncate(0); err != nil {
			return fmt.Errorf("failed to truncate destination for download retry: %w", err)
		}
	}
	return nil
}

// GetProductByName finds a product by name
func (c *Client) GetProductByName(ctx context.Context, name string) (*Product, error) {
	products, err := c.ListProducts(ctx)
	if err != nil {
		return nil, err
	}

	for _, p := range products {
		if strings.EqualFold(p.Name, name) {
			return p, nil
		}
	}

	return nil, &NotFoundError{
		Resource: "product",
		ID:       name,
	}
}

// GetLatestDelivery returns the most recent delivery for a product
func (c *Client) GetLatestDelivery(ctx context.Context, productID int) (*Delivery, error) {
	product, err := c.GetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}

	if len(product.Deliveries) == 0 {
		return nil, &NotFoundError{
			Resource: "delivery",
			ID:       fmt.Sprintf("product %d has no deliveries", productID),
		}
	}

	// Find the latest data delivery. EPO mixes administrative/notification
	// deliveries (e.g. "NOTIFICATION: NEW DTD ...") into the same list; those are
	// noisy and not representative of the product's actual data, so prefer real
	// data deliveries and only fall back to the overall latest if none qualify.
	var latest, latestData *Delivery
	for _, d := range product.Deliveries {
		if latest == nil || d.DeliveryPublicationDatetime.After(latest.DeliveryPublicationDatetime) {
			latest = d
		}
		if isNotificationDelivery(d.DeliveryName) {
			continue
		}
		if latestData == nil || d.DeliveryPublicationDatetime.After(latestData.DeliveryPublicationDatetime) {
			latestData = d
		}
	}

	if latestData != nil {
		return latestData, nil
	}
	return latest, nil
}

// isNotificationDelivery reports whether a delivery is an administrative or
// notification entry rather than an actual data delivery.
func isNotificationDelivery(name string) bool {
	upper := strings.ToUpper(name)
	for _, marker := range []string{"NOTIFICATION", "INFO:", "ANNOUNCEMENT"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	return false
}
