# EPO BDDS Go Client

[![CI](https://github.com/patent-dev/epo-bdds/actions/workflows/ci.yml/badge.svg)](https://github.com/patent-dev/epo-bdds/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/patent-dev/epo-bdds.svg)](https://pkg.go.dev/github.com/patent-dev/epo-bdds)
[![Go Report Card](https://goreportcard.com/badge/github.com/patent-dev/epo-bdds)](https://goreportcard.com/report/github.com/patent-dev/epo-bdds)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A Go client for the European Patent Office's Bulk Data Distribution Service
(BDDS) REST API - product discovery, delivery listing, and downloading of bulk
datasets such as DOCDB, INPADOC, and EP full-text.

## Overview

- **Product discovery** - list products, fetch a product with its deliveries,
  look up a product by name, get the latest delivery.
- **File downloads** - stream a delivery file to any `io.Writer`, with an
  optional progress callback for large files.
- **Automatic auth** - OAuth2 password grant; tokens are cached and refreshed
  before expiry. Credentials are optional: free/public products work without them.
- **Robust requests** - retry with backoff and automatic re-authentication on
  expiry; typed errors for the common failure cases.

## Installation

```bash
go get github.com/patent-dev/epo-bdds
```

## Getting access

Free/public BDDS products can be listed and downloaded without an account. Paid
products require an EPO account with a subscription to the relevant product; the
client then authenticates via OAuth2 password grant.

1. Open the [BDDS portal](https://publication-bdds.apps.epo.org) to browse the
   product catalogue, then start sign-in / registration.

2. Sign in with your EPO account, or create one at the
   [EPO login](https://login.epo.org).

3. Subscribe to the products you need (free/public products need no subscription).

4. Export your EPO credentials for the client and demo:
   ```bash
   export EPO_BDDS_USERNAME=...
   export EPO_BDDS_PASSWORD=...
   ```


## Quick start

```go
package main

import (
    "context"
    "fmt"
    "log"

    bdds "github.com/patent-dev/epo-bdds"
)

func main() {
    // Credentials are optional: free/public products (including ListProducts)
    // work without them. Pass a Config with Username/Password for paid products.
    client, err := bdds.NewClient(nil)
    if err != nil {
        log.Fatal(err)
    }

    products, err := client.ListProducts(context.Background())
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Found %d products\n", len(products))
    for _, p := range products {
        fmt.Printf("- [%d] %s\n", p.ID, p.Name)
    }
}
```

## Usage

### Configuration

```go
config := &bdds.Config{
    Username:   "your-username",                          // optional, needed for paid products
    Password:   "your-password",                          // optional, needed for paid products
    BaseURL:    "https://publication-bdds.apps.epo.org",  // default
    UserAgent:  "YourApp/1.0",                            // optional
    MaxRetries: 3,                                        // default: 3
    RetryDelay: time.Second,                              // delay between retries, default: 1s
    Timeout:    30 * time.Second,                         // request timeout, default: 30s
}

client, err := bdds.NewClient(config)
```

`RetryDelay` and `Timeout` are `time.Duration` values.

### Product discovery

```go
// List all available products.
products, err := client.ListProducts(ctx)

// Get a product with its deliveries.
product, err := client.GetProduct(ctx, 3) // EP DocDB front file

// Find a product by name.
product, err := client.GetProductByName(ctx, "EP DocDB front file")

// Get the most recent delivery for a product.
delivery, err := client.GetLatestDelivery(ctx, 3)
```

`GetProduct` returns a `*ProductWithDeliveries`; each delivery lists its files:

```go
for _, d := range product.Deliveries {
    fmt.Printf("%s - %d files\n", d.DeliveryName, len(d.Files))
}
```

### File downloads

If you already have the product, delivery, and file IDs, downloads work without
credentials:

```go
f, err := os.Create("download.zip")
if err != nil {
    log.Fatal(err)
}
defer f.Close()

err = client.DownloadFile(ctx, productID, deliveryID, fileID, f)
```

Use `DownloadFileWithProgress` for a progress callback on large files:

```go
err = client.DownloadFileWithProgress(ctx, 3, 12345, 67890, f,
    func(bytesWritten, totalBytes int64) {
        fmt.Printf("\rProgress: %.1f%%", float64(bytesWritten)*100/float64(totalBytes))
    })
```

### Common product IDs

| ID | Name | Description |
|----|------|-------------|
| 3  | EP DocDB front file | Bibliographic data (front file) |
| 4  | EP full-text data - front file | Full-text patent data |
| 14 | EP DocDB back file | Bibliographic data (back file) |
| 17 | PATSTAT Global | Patent statistics database |
| 18 | PATSTAT EP Register | EP register data |

### Demo

An interactive demo exercises every method. Set credentials and run it:

```bash
export EPO_BDDS_USERNAME=your-username
export EPO_BDDS_PASSWORD=your-password
cd demo && go run demo.go
```

See [demo/README.md](demo/README.md) for details.

## Error handling

The library returns typed errors you can match with `errors.As`:

```go
var authErr *bdds.AuthError
if errors.As(err, &authErr) {
    fmt.Printf("auth failed (status %d): %s\n", authErr.StatusCode, authErr.Message)
}

var notFound *bdds.NotFoundError
if errors.As(err, &notFound) {
    fmt.Printf("%s not found: %s\n", notFound.Resource, notFound.ID)
}

var rateLimit *bdds.RateLimitError
if errors.As(err, &rateLimit) {
    fmt.Printf("rate limited, retry after %d seconds\n", rateLimit.RetryAfter)
}
```

## Testing

```bash
make test              # unit tests (mock HTTP server, race)
make test-integration  # integration tests against the real API, needs credentials
make lint
```

Integration tests require valid EPO BDDS credentials and skip gracefully when
they are not set or a product is not accessible for your account:

```bash
export EPO_BDDS_USERNAME=your-username
export EPO_BDDS_PASSWORD=your-password
```

## Regenerating from OpenAPI

The typed code under `generated/` is produced from the hand-crafted `openapi.yaml`
with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen). If you change
the spec, regenerate it:

```bash
make generate
```

## Related projects

Part of the [patent.dev](https://patent.dev) open-source patent data ecosystem:

- [epo-ops](https://github.com/patent-dev/epo-ops) - EPO Open Patent Services client (bibliographic, full text, families, legal status, images)
- [uspto-odp](https://github.com/patent-dev/uspto-odp) - USPTO Open Data Portal client (patents, PTAB, TSDR, full text)
- [dpma-connect-plus](https://github.com/patent-dev/dpma-connect-plus) - DPMA Connect Plus client (patents, designs, trademarks)

The [bulk-file-loader](https://github.com/patent-dev/bulk-file-loader) uses these libraries for automated patent data downloads.

## License

MIT - Funktionslust GmbH / patent.dev.
