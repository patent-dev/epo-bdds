//go:build integration

package bdds

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIntegration_RequiresCredentials checks that credentials are set
func TestIntegration_RequiresCredentials(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD environment variables required for integration tests")
	}
}

// TestIntegration_Authentication tests authentication with real API
func TestIntegration_Authentication(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD required")
	}

	config := &Config{
		Username: username,
		Password: password,
		Timeout:  30,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Force authentication
	err = client.authenticate(ctx)
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	if client.token == "" {
		t.Error("Expected token to be set after authentication")
	}

	t.Logf("✓ Authentication successful")
	t.Logf("Token expires at: %s", client.tokenExpiry.Format(time.RFC3339))
}

// TestIntegration_ListProducts tests listing products with real API
func TestIntegration_ListProducts(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD required")
	}

	config := &Config{
		Username: username,
		Password: password,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	products, err := client.ListProducts(ctx)
	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}

	if len(products) == 0 {
		t.Error("Expected at least one product")
	}

	t.Logf("✓ Found %d products:", len(products))
	for _, p := range products {
		t.Logf("  - [ID: %d] %s", p.ID, p.Name)
	}
}

// TestIntegration_GetProduct tests getting product details with real API
func TestIntegration_GetProduct(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD required")
	}

	// Test with known product IDs
	testProducts := []int{3, 4, 14} // DocDB front, Full-text front, DocDB back

	config := &Config{
		Username: username,
		Password: password,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, productID := range testProducts {
		t.Run(string(rune(productID+'0')), func(t *testing.T) {
			product, err := client.GetProduct(ctx, productID)
			if err != nil {
				// Product might not be accessible if not subscribed
				t.Logf("Note: Could not access product %d (might not be subscribed): %v", productID, err)
				return
			}

			t.Logf("✓ Product %d: %s", product.ID, product.Name)
			t.Logf("  Deliveries: %d", len(product.Deliveries))

			if len(product.Deliveries) > 0 {
				latest := product.Deliveries[0]
				t.Logf("  Latest delivery: %s (%d files)", latest.DeliveryName, len(latest.Files))
			}
		})
	}
}

// TestIntegration_GetProductByName tests finding product by name
func TestIntegration_GetProductByName(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD required")
	}

	config := &Config{
		Username: username,
		Password: password,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try to find a product by partial name
	products, err := client.ListProducts(ctx)
	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}

	if len(products) == 0 {
		t.Skip("No products available")
	}

	// Try to find the first product by name
	targetName := products[0].Name
	product, err := client.GetProductByName(ctx, targetName)
	if err != nil {
		t.Fatalf("GetProductByName failed: %v", err)
	}

	if product.Name != targetName {
		t.Errorf("Expected product name %s, got %s", targetName, product.Name)
	}

	t.Logf("✓ Found product by name: %s", product.Name)
}

// TestIntegration_GetLatestDelivery tests getting latest delivery
func TestIntegration_GetLatestDelivery(t *testing.T) {
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		t.Skip("EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD required")
	}

	config := &Config{
		Username: username,
		Password: password,
	}

	client, err := NewClient(config)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Try product 3 (DocDB front file)
	delivery, err := client.GetLatestDelivery(ctx, 3)
	if err != nil {
		t.Logf("Note: Could not get latest delivery for product 3: %v", err)
		t.Skip("Product might not be accessible")
	}

	t.Logf("✓ Latest delivery for product 3:")
	t.Logf("  Name: %s", delivery.DeliveryName)
	t.Logf("  Published: %s", delivery.DeliveryPublicationDatetime.Format(time.RFC3339))
	t.Logf("  Files: %d", len(delivery.Files))
}

// Note: File download test is intentionally skipped by default to avoid
// downloading large files during normal testing. To test file downloads,
// create a separate manual test or use a small test file if available.
