package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/patent-dev/epo-bdds"
)

// maxExampleFileBytes caps the size of a real file download recorded as an
// example, to keep the repository small. Only files at or below this size are
// downloaded; anything larger is recorded as metadata only.
const maxExampleFileBytes = 3 * 1024 * 1024 // 3 MB

// recordExamples fetches the JSON metadata endpoints and saves them under
// demo/examples for documentation and the weekly response-watch diff. It records
// metadata (product list, product detail, latest delivery) and, when one is
// available, a single small real file download. It never downloads bulk data
// files.
func recordExamples(ctx context.Context, client *bdds.Client) error {
	products, err := client.ListProducts(ctx)
	if err != nil {
		return fmt.Errorf("list products: %w", err)
	}
	if err := saveExampleJSON("list_products", "GET /products", products); err != nil {
		return err
	}
	if len(products) == 0 {
		return nil
	}

	// Use the first product as a representative detail/delivery example.
	id := products[0].ID
	product, err := client.GetProduct(ctx, id)
	if err != nil {
		return fmt.Errorf("get product %d: %w", id, err)
	}
	if err := saveExampleJSON("get_product", fmt.Sprintf("GET /products/%d", id), product); err != nil {
		return err
	}

	delivery, err := client.GetLatestDelivery(ctx, id)
	if err != nil {
		return fmt.Errorf("get latest delivery for product %d: %w", id, err)
	}
	if err := saveExampleJSON("get_latest_delivery", fmt.Sprintf("latest delivery for product %d", id), delivery); err != nil {
		return err
	}

	if err := recordSmallFile(ctx, client, products); err != nil {
		return err
	}
	return nil
}

// recordSmallFile finds the smallest accessible file across the catalogue and,
// if it is small enough, downloads it under demo/examples/download_file. Larger
// candidates are recorded as metadata only. Products likely to hold small files
// (samples, DTD/schema repositories) are inspected first.
func recordSmallFile(ctx context.Context, client *bdds.Client, products []*bdds.Product) error {
	type candidate struct {
		productID  int
		deliveryID int
		file       *bdds.DeliveryFile
		bytes      int64
	}

	var best *candidate
	for _, p := range preferredProductOrder(products) {
		product, err := client.GetProduct(ctx, p.ID)
		if err != nil {
			// Not subscribed / not accessible: skip.
			continue
		}
		for _, d := range product.Deliveries {
			for _, f := range d.Files {
				size := parseHumanSize(f.FileSize)
				if size <= 0 {
					continue
				}
				if best == nil || size < best.bytes {
					best = &candidate{productID: p.ID, deliveryID: d.DeliveryID, file: f, bytes: size}
				}
			}
		}
		// A small enough file from a preferred product is good enough; stop early.
		if best != nil && best.bytes <= maxExampleFileBytes {
			break
		}
	}

	if best == nil {
		fmt.Println("no downloadable files found for the download example")
		return nil
	}

	meta := map[string]any{
		"productId":  best.productID,
		"deliveryId": best.deliveryID,
		"fileId":     best.file.FileID,
		"fileName":   best.file.FileName,
		"fileSize":   best.file.FileSize,
		"checksum":   best.file.FileChecksum,
	}

	if best.bytes > maxExampleFileBytes {
		meta["note"] = fmt.Sprintf("file exceeds %d bytes; metadata recorded only, content not downloaded", maxExampleFileBytes)
		return saveExampleJSON("download_file",
			fmt.Sprintf("GET /products/%d/deliveries/%d/files/%d (metadata only)", best.productID, best.deliveryID, best.file.FileID),
			meta)
	}

	var buf bytes.Buffer
	if err := client.DownloadFile(ctx, best.productID, best.deliveryID, best.file.FileID, &buf); err != nil {
		return fmt.Errorf("download file %d: %w", best.file.FileID, err)
	}
	meta["downloadedBytes"] = buf.Len()

	dir := filepath.Join("examples", "download_file")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, best.file.FileName), buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := saveExampleJSON("download_file",
		fmt.Sprintf("GET /products/%d/deliveries/%d/files/%d", best.productID, best.deliveryID, best.file.FileID),
		meta); err != nil {
		return err
	}
	fmt.Printf("downloaded small example file %s (%d bytes)\n", best.file.FileName, buf.Len())
	return nil
}

// preferredProductOrder sorts products so those most likely to hold small files
// (samples, DTD/schema repositories) are inspected first.
func preferredProductOrder(products []*bdds.Product) []*bdds.Product {
	out := make([]*bdds.Product, len(products))
	copy(out, products)
	rank := func(p *bdds.Product) int {
		name := strings.ToLower(p.Name)
		switch {
		case strings.Contains(name, "sample"):
			return 0
		case strings.Contains(name, "dtd"), strings.Contains(name, "schema"):
			return 1
		case strings.Contains(name, "coverage"), strings.Contains(name, "statistics"):
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// parseHumanSize converts a human-readable size such as "17.7 kB" or "1.5 GB"
// to a byte count. It returns 0 when the value cannot be parsed.
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

func saveExampleJSON(name, request string, v any) error {
	dir := filepath.Join("examples", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "request.txt"), []byte(request+"\n"), 0o600); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "response.json"), append(data, '\n'), 0o600); err != nil {
		return err
	}
	fmt.Printf("recorded examples/%s\n", name)
	return nil
}
