# EPO BDDS Go Client

[![Go Reference](https://pkg.go.dev/badge/github.com/patent-dev/epo-bdds.svg)](https://pkg.go.dev/github.com/patent-dev/epo-bdds)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

A Go client library for the European Patent Office Bulk Data Distribution Service (BDDS).

## Getting Started

### Authentication

Authentication requirements depend on the data you're accessing:

- **Free/Public Products**: Listing and downloading work without authentication
- **Paid Products**: Require EPO BDDS subscription and authentication for listing and downloading (see [pricing](https://www.epo.org/en/service-support/ordering/patent-knowledge-products-services))

**Authentication Method**: OAuth2 password grant flow

## Installation

```bash
go get github.com/patent-dev/epo-bdds
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/patent-dev/epo-bdds"
)

func main() {
    // Create client
    client, err := bdds.NewClient(&bdds.Config{
        Username: "your-epo-username",
        Password: "your-epo-password",
    })
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // List all products
    products, err := client.ListProducts(ctx)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Found %d products\n", len(products))
    for _, p := range products {
        fmt.Printf("- [%d] %s\n", p.ID, p.Name)
    }
}
```

## API Methods

### Product Discovery

```go
// List all available products
ListProducts(ctx context.Context) ([]*Product, error)

// Get product details with deliveries
GetProduct(ctx context.Context, productID int) (*ProductWithDeliveries, error)

// Find product by name
GetProductByName(ctx context.Context, name string) (*Product, error)

// Get most recent delivery for a product
GetLatestDelivery(ctx context.Context, productID int) (*Delivery, error)
```

### File Downloads

```go
// Download file to writer
DownloadFile(ctx context.Context, productID, deliveryID, fileID int, dst io.Writer) error

// Download with progress callback
DownloadFileWithProgress(ctx context.Context, productID, deliveryID, fileID int,
    dst io.Writer, progressFn func(bytesWritten, totalBytes int64)) error
```

## Configuration

```go
config := &bdds.Config{
    Username:   "your-username",          // Optional (needed for paid products)
    Password:   "your-password",          // Optional (needed for paid products)
    BaseURL:    "https://publication-bdds.apps.epo.org", // Default
    UserAgent:  "YourApp/1.0",           // Optional
    MaxRetries: 3,                        // Default: 3
    RetryDelay: 1,                        // Seconds between retries, default: 1
    Timeout:    30,                       // Request timeout in seconds, default: 30
}

client, err := bdds.NewClient(config)
```

## Features

### Automatic Token Management
- OAuth2 authentication handled automatically
- Tokens cached and refreshed before expiry
- No manual token management required

### Robust Error Handling
- Automatic retry with exponential backoff
- Graceful handling of rate limits
- Custom error types for different scenarios

### Progress Tracking
- Download progress callbacks for large files
- Real-time byte tracking during downloads

## Usage Examples

### Download Without Authentication

If you only need to download files and already have the product/delivery/file IDs:

```go
// Create client without credentials
client, err := bdds.NewClient(&bdds.Config{})
if err != nil {
    log.Fatal(err)
}

// Download file directly
file, err := os.Create("download.zip")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

err = client.DownloadFile(ctx, productID, deliveryID, fileID, file)
if err != nil {
    log.Fatal(err)
}
```

### List Products

```go
products, err := client.ListProducts(ctx)
if err != nil {
    log.Fatal(err)
}

for _, p := range products {
    fmt.Printf("Product %d: %s\n", p.ID, p.Name)
    fmt.Printf("  %s\n", p.Description)
}
```

### Get Product with Deliveries

```go
product, err := client.GetProduct(ctx, 3) // EP DocDB front file
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Product: %s\n", product.Name)
fmt.Printf("Deliveries: %d\n", len(product.Deliveries))

for _, delivery := range product.Deliveries {
    fmt.Printf("  %s - %d files\n", delivery.DeliveryName, len(delivery.Files))
}
```

### Download File with Progress

```go
file, err := os.Create("download.zip")
if err != nil {
    log.Fatal(err)
}
defer file.Close()

err = client.DownloadFileWithProgress(ctx, 3, 12345, 67890, file,
    func(bytesWritten, totalBytes int64) {
        percent := float64(bytesWritten) * 100 / float64(totalBytes)
        fmt.Printf("\rProgress: %.1f%%", percent)
    })
if err != nil {
    log.Fatal(err)
}
```

### Find Product by Name

```go
product, err := client.GetProductByName(ctx, "EP DocDB front file")
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Found product: %d - %s\n", product.ID, product.Name)
```

### Get Latest Delivery

```go
delivery, err := client.GetLatestDelivery(ctx, 3)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Latest delivery: %s\n", delivery.DeliveryName)
fmt.Printf("Published: %s\n", delivery.DeliveryPublicationDatetime)
fmt.Printf("Files: %d\n", len(delivery.Files))
```

## Common Product IDs

| ID | Name | Description |
|----|------|-------------|
| 3  | EP DocDB front file | Bibliographic data (front file) |
| 4  | EP full-text data - front file | Full-text patent data |
| 14 | EP DocDB back file | Bibliographic data (back file) |
| 17 | PATSTAT Global | Patent statistics database |
| 18 | PATSTAT EP Register | EP register data |

## Error Handling

The library provides custom error types for different scenarios:

```go
// Authentication errors
if authErr, ok := err.(*bdds.AuthError); ok {
    fmt.Printf("Auth failed: %s\n", authErr.Message)
}

// Not found errors
if notFoundErr, ok := err.(*bdds.NotFoundError); ok {
    fmt.Printf("Resource not found: %s\n", notFoundErr.ID)
}

// Rate limit errors
if rateLimitErr, ok := err.(*bdds.RateLimitError); ok {
    fmt.Printf("Rate limited, retry after %d seconds\n", rateLimitErr.RetryAfter)
}
```

## Testing

This library includes comprehensive test coverage:

### Unit Tests (Mock Server)

Offline tests using mock HTTP server with realistic responses:

```bash
# Run unit tests
go test -v

# Run with coverage
go test -v -cover

# Generate coverage report
go test -cover -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Integration Tests (Real API)

Tests that make actual requests to the EPO BDDS API:

```bash
# Set credentials
export EPO_BDDS_USERNAME=your-username
export EPO_BDDS_PASSWORD=your-password

# Run integration tests
go test -tags=integration -v

# Run specific test
go test -tags=integration -v -run TestIntegration_ListProducts
```

**Note**: Integration tests require valid EPO BDDS credentials and will fail gracefully if not set or if specific products are not accessible (based on account).

## Implementation

This library follows a clean architecture:

1. **OpenAPI Specification**: Unofficial hand-crafted `openapi.yaml` based on actual API behavior
2. **Code Generation**: Types and client generated using [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen)
3. **Idiomatic Wrapper**: Clean Go client wrapping generated code
4. **Automatic Auth**: OAuth2 token management handled transparently

### Package Structure

```
├── client.go           # Main client implementation
├── client_test.go     # Unit tests with mock server
├── integration_test.go # Integration tests with real API
├── types.go           # Public types
├── errors.go          # Custom error types
├── utils.go           # Internal utilities
├── generated/         # Auto-generated code
│   ├── types_gen.go   # Generated types
│   └── client_gen.go  # Generated client
└── openapi.yaml      # OpenAPI 3.0 specification
```

## Demo Application

An interactive demo application is included to showcase all library features:

```bash
# Set credentials
export EPO_BDDS_USERNAME=your-username
export EPO_BDDS_PASSWORD=your-password

# Run demo
cd demo
go run demo.go
```

The demo provides an interactive menu for:
- Listing all products
- Viewing product details with deliveries
- Finding products by name
- Getting latest delivery information
- Downloading files with progress tracking

See [demo/README.md](demo/README.md) for full documentation.

## Development

### Regenerating from OpenAPI

If the OpenAPI spec is updated:

```bash
# Install generator
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# Generate types
oapi-codegen -package generated -generate types openapi.yaml > generated/types_gen.go

# Generate client
oapi-codegen -package generated -generate client openapi.yaml > generated/client_gen.go
```

## Similar Projects

This project follows the style and quality standards of:
- [patent-dev/uspto-odp](https://github.com/patent-dev/uspto-odp) - USPTO Open Data Portal Go client

## License

MIT License - see [LICENSE](LICENSE) file for details.

## Credits

**Developed by:**
- Wolfgang Stark - [patent.dev](https://patent.dev) - [Funktionslust GmbH](https://funktionslust.digital)