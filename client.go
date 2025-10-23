package bdds

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/patent-dev/epo-bdds/generated"
)

const (
	// OAuth2 token TTL from EPO BDDS documentation
	tokenTTL = time.Hour
	// Refresh token 5 minutes before expiry for safety
	tokenRefreshBuffer = 5 * time.Minute
)

// Client is the main EPO BDDS API client
type Client struct {
	config          *Config
	httpClient      *http.Client
	token           string
	tokenExpiry     time.Time
	generatedClient *generated.ClientWithResponses
}

// Config holds client configuration
type Config struct {
	Username   string // EPO BDDS username
	Password   string // EPO BDDS password
	BaseURL    string // Base URL for API (default: https://publication-bdds.apps.epo.org)
	UserAgent  string // Optional custom user agent
	MaxRetries int    // Maximum number of retries (default: 3)
	RetryDelay int    // Seconds between retries (default: 1)
	Timeout    int    // Request timeout in seconds (default: 30)
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		BaseURL:    "https://publication-bdds.apps.epo.org",
		UserAgent:  "PatentDev/BDDS/1.0",
		MaxRetries: 3,
		RetryDelay: 1,
		Timeout:    30,
	}
}

// NewClient creates a new EPO BDDS API client
// Authentication is optional - free products work without credentials,
// paid products require EPO BDDS subscription and authentication.
func NewClient(config *Config) (*Client, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Apply defaults
	if config.BaseURL == "" {
		config.BaseURL = DefaultConfig().BaseURL
	}
	if config.UserAgent == "" {
		config.UserAgent = DefaultConfig().UserAgent
	}
	if config.MaxRetries == 0 {
		config.MaxRetries = DefaultConfig().MaxRetries
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = DefaultConfig().RetryDelay
	}
	if config.Timeout == 0 {
		config.Timeout = DefaultConfig().Timeout
	}

	httpClient := &http.Client{
		Timeout: time.Duration(config.Timeout) * time.Second,
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
		if err := c.ensureValidToken(ctx); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	req.Header.Set("User-Agent", c.config.UserAgent)
	return nil
}

// ensureValidToken checks if token is expired and refreshes if needed
func (c *Client) ensureValidToken(ctx context.Context) error {
	// Skip if no credentials configured
	if c.config.Username == "" || c.config.Password == "" {
		return nil
	}

	// Check if token exists and is still valid
	if c.token != "" && time.Now().Add(tokenRefreshBuffer).Before(c.tokenExpiry) {
		return nil
	}

	// Need to authenticate or refresh
	return c.authenticate(ctx)
}

// authenticate performs OAuth2 password grant authentication
func (c *Client) authenticate(ctx context.Context) error {
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
	defer resp.Body.Close()

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

	c.token = tokenResp.AccessToken
	c.tokenExpiry = time.Now().Add(tokenTTL)

	return nil
}

// retryableRequest wraps requests with retry logic
func (c *Client) retryableRequest(ctx context.Context, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		lastErr = err

		// Check if it's a 401 - try re-authenticating
		if authErr, ok := err.(*AuthError); ok && authErr.StatusCode == 401 && attempt < c.config.MaxRetries {
			// Force re-auth by clearing token
			c.token = ""
			c.tokenExpiry = time.Time{}
		}

		if attempt < c.config.MaxRetries {
			time.Sleep(time.Duration(c.config.RetryDelay*(attempt+1)) * time.Second)
		}
	}
	return fmt.Errorf("failed after %d retries: %w", c.config.MaxRetries, lastErr)
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
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
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
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode(), string(resp.Body))
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

// DownloadFileWithProgress downloads a file to the provided writer with progress callback
func (c *Client) DownloadFileWithProgress(ctx context.Context, productID, deliveryID, fileID int, dst io.Writer, progressFn func(bytesWritten, totalBytes int64)) error {
	return c.retryableRequest(ctx, func() error {
		resp, err := c.generatedClient.DownloadFile(ctx, productID, deliveryID, fileID)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			return &NotFoundError{
				Resource: "file",
				ID:       fmt.Sprintf("%d/%d/%d", productID, deliveryID, fileID),
			}
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
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

		_, err = io.Copy(dst, reader)
		return err
	})
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

	// Find the delivery with the latest publication date
	latest := product.Deliveries[0]
	for _, d := range product.Deliveries[1:] {
		if d.DeliveryPublicationDatetime.After(latest.DeliveryPublicationDatetime) {
			latest = d
		}
	}

	return latest, nil
}

