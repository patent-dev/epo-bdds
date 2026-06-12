//go:build integration

package bdds_test

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	bdds "github.com/patent-dev/epo-bdds"
)

// This file holds one live integration test per EPO BDDS convenience method (all
// 6), each named TestIntegration<MethodName> so it maps 1:1 to the Client methods
// in client.go (verified by scripts/check-integration-coverage.sh).
// Run with: go test -tags=integration -count=1 ./...
//
// Every test PASSES or SKIPS, never FAILS on a documented condition:
//   - missing credentials skip the whole suite (testClient);
//   - 401/403 (not subscribed), 404 (resource gone) and 429 (rate limited) skip
//     cleanly via skipExpected, so an unprovisioned account stays green.
//
// BDDS files are bulk data and can be gigabytes. The two Download* tests therefore
// locate the SMALLEST accessible file across the catalogue (smallestFile) and
// stream it to a discarding byte counter; they skip if no small-enough file is
// available, and NEVER download a full bulk file.

// maxDownloadBytes caps the size of a file the Download* integration tests will
// fetch, so a live run never pulls a multi-GB bulk file. Only files at or below
// this human-readable size are considered.
const maxDownloadBytes = 5 * 1024 * 1024 // 5 MB

// testClient builds a live client from EPO_BDDS_USERNAME / EPO_BDDS_PASSWORD, or
// skips the test when either is absent so the suite stays green without creds.
func testClient(t *testing.T) *bdds.Client {
	t.Helper()
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")
	if username == "" || password == "" {
		t.Skip("set EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD to run EPO BDDS integration tests")
	}
	client, err := bdds.NewClient(&bdds.Config{
		Username: username,
		Password: password,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

// testContext returns a context with a per-test timeout. Downloads get a longer
// budget than metadata calls.
func testContext(t *testing.T, d time.Duration) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	t.Cleanup(cancel)
	return ctx
}

// skipExpected turns a documented "not available for this account" outcome into a
// clean SKIP instead of a FAIL: 401/403 (not subscribed), 404 (resource gone) and
// 429 (rate limited). Any other error fails the test. A nil error is a no-op.
func skipExpected(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var authErr *bdds.AuthError
	if errors.As(err, &authErr) {
		t.Skipf("auth/forbidden (status %d): account not provisioned for this resource", authErr.StatusCode)
	}
	var notFound *bdds.NotFoundError
	if errors.As(err, &notFound) {
		t.Skipf("not found: %v", err)
	}
	var rate *bdds.RateLimitError
	if errors.As(err, &rate) {
		t.Skipf("rate limited: %v", err)
	}
	// 403 (and any other non-typed status) surfaces as a generic status error
	// whose message embeds the code; treat 403 as a clean skip too.
	if strings.Contains(err.Error(), "status 403") {
		t.Skipf("forbidden: %v", err)
	}
	t.Fatalf("unexpected error: %v", err)
}

// smallestFile scans the accessible catalogue for the smallest downloadable file
// at or below maxDownloadBytes and returns its coordinates. It skips the test if
// the product list is empty or no small-enough file is accessible. Products
// likely to hold small files (samples, DTD/schema repositories) are inspected
// first so the scan stays cheap.
func smallestFile(ctx context.Context, t *testing.T, client *bdds.Client) (productID, deliveryID, fileID int, size string) {
	t.Helper()
	products, err := client.ListProducts(ctx)
	skipExpected(t, err)
	if len(products) == 0 {
		t.Skip("no products accessible to this account")
	}

	type candidate struct {
		productID, deliveryID, fileID int
		bytes                         int64
		size                          string
	}
	var best *candidate
	for _, p := range preferredOrder(products) {
		product, err := client.GetProduct(ctx, p.ID)
		if err != nil {
			// Not subscribed / not accessible: skip this product, keep scanning.
			continue
		}
		for _, d := range product.Deliveries {
			for _, f := range d.Files {
				b := parseHumanSize(f.FileSize)
				if b <= 0 {
					continue
				}
				if best == nil || b < best.bytes {
					best = &candidate{p.ID, d.DeliveryID, f.FileID, b, f.FileSize}
				}
			}
		}
		// A small enough file from a preferred product is good enough; stop early.
		if best != nil && best.bytes <= maxDownloadBytes {
			break
		}
	}

	if best == nil || best.bytes > maxDownloadBytes {
		t.Skipf("no file <= %d bytes accessible for the download test", maxDownloadBytes)
	}
	return best.productID, best.deliveryID, best.fileID, best.size
}

// preferredOrder sorts products so those most likely to hold small files
// (samples, DTD/schema repositories) are inspected first.
func preferredOrder(products []*bdds.Product) []*bdds.Product {
	out := make([]*bdds.Product, len(products))
	copy(out, products)
	rank := func(p *bdds.Product) int {
		name := strings.ToLower(p.Name)
		switch {
		case strings.Contains(name, "sample"):
			return 0
		case strings.Contains(name, "dtd"), strings.Contains(name, "schema"):
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// parseHumanSize converts a human-readable size such as "17.7 kB" to a byte
// count, returning 0 when the value cannot be parsed.
func parseHumanSize(s string) int64 {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) != 2 {
		return 0
	}
	value, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	mult := map[string]float64{
		"B":  1,
		"KB": 1 << 10, "KIB": 1 << 10,
		"MB": 1 << 20, "MIB": 1 << 20,
		"GB": 1 << 30, "GIB": 1 << 30,
		"TB": 1 << 40, "TIB": 1 << 40,
	}
	m, ok := mult[strings.ToUpper(fields[1])]
	if !ok {
		return 0
	}
	return int64(value * m)
}

// countingWriter discards bytes while counting them, so a download test can
// assert non-empty bytes were written without buffering a whole file.
type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// firstAccessibleProduct returns a product id whose details this account can read,
// skipping the test if none is accessible. Used by the metadata tests that need a
// concrete, reachable id.
func firstAccessibleProduct(ctx context.Context, t *testing.T, client *bdds.Client) int {
	t.Helper()
	products, err := client.ListProducts(ctx)
	skipExpected(t, err)
	if len(products) == 0 {
		t.Skip("no products accessible to this account")
	}
	for _, p := range products {
		if _, err := client.GetProduct(ctx, p.ID); err == nil {
			return p.ID
		}
	}
	t.Skip("no product details accessible to this account")
	return 0
}

// --- Metadata endpoints ---------------------------------------------------

func TestIntegrationListProducts(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 30*time.Second)

	products, err := client.ListProducts(ctx)
	skipExpected(t, err)
	if len(products) == 0 {
		t.Fatal("ListProducts returned no products")
	}
	if products[0].Name == "" {
		t.Fatalf("first product has empty Name: %+v", products[0])
	}
}

func TestIntegrationGetProduct(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 30*time.Second)

	id := firstAccessibleProduct(ctx, t, client)
	product, err := client.GetProduct(ctx, id)
	skipExpected(t, err)
	if product == nil {
		t.Fatal("GetProduct returned nil")
	}
	if product.ID != id {
		t.Fatalf("GetProduct id = %d, want %d", product.ID, id)
	}
}

func TestIntegrationGetProductByName(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 30*time.Second)

	products, err := client.ListProducts(ctx)
	skipExpected(t, err)
	if len(products) == 0 {
		t.Skip("no products accessible to this account")
	}
	want := products[0].Name
	product, err := client.GetProductByName(ctx, want)
	skipExpected(t, err)
	if product == nil {
		t.Fatal("GetProductByName returned nil")
	}
	if !strings.EqualFold(product.Name, want) {
		t.Fatalf("GetProductByName name = %q, want %q", product.Name, want)
	}
}

func TestIntegrationGetLatestDelivery(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 30*time.Second)

	id := firstAccessibleProduct(ctx, t, client)
	delivery, err := client.GetLatestDelivery(ctx, id)
	skipExpected(t, err)
	if delivery == nil {
		t.Fatal("GetLatestDelivery returned nil")
	}
	if delivery.DeliveryName == "" {
		t.Fatalf("latest delivery has empty DeliveryName: %+v", delivery)
	}
}

// --- Streaming endpoints --------------------------------------------------

func TestIntegrationDownloadFile(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 2*time.Minute)

	productID, deliveryID, fileID, size := smallestFile(ctx, t, client)
	t.Logf("downloading smallest accessible file: product %d delivery %d file %d (%s)",
		productID, deliveryID, fileID, size)

	var w countingWriter
	err := client.DownloadFile(ctx, productID, deliveryID, fileID, &w)
	skipExpected(t, err)
	if w.n == 0 {
		t.Fatal("DownloadFile wrote 0 bytes")
	}
}

func TestIntegrationDownloadFileWithProgress(t *testing.T) {
	client := testClient(t)
	ctx := testContext(t, 2*time.Minute)

	productID, deliveryID, fileID, size := smallestFile(ctx, t, client)
	t.Logf("downloading smallest accessible file: product %d delivery %d file %d (%s)",
		productID, deliveryID, fileID, size)

	var w countingWriter
	var progressCalls int
	progressFn := func(_, _ int64) { progressCalls++ }

	err := client.DownloadFileWithProgress(ctx, productID, deliveryID, fileID, &w, progressFn)
	skipExpected(t, err)
	if w.n == 0 {
		t.Fatal("DownloadFileWithProgress wrote 0 bytes")
	}
	if progressCalls == 0 {
		t.Fatal("progressFn was never called")
	}
}
