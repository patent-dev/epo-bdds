package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/patent-dev/epo-bdds"
)

func main() {
	fmt.Println("EPO BDDS API Client Demo")
	fmt.Println("=========================")
	fmt.Println()

	// Get credentials
	username := os.Getenv("EPO_BDDS_USERNAME")
	password := os.Getenv("EPO_BDDS_PASSWORD")

	if username == "" || password == "" {
		fmt.Println("Error: EPO BDDS credentials not found in environment")
		fmt.Println()
		fmt.Println("Please set the following environment variables:")
		fmt.Println("  export EPO_BDDS_USERNAME=your-username")
		fmt.Println("  export EPO_BDDS_PASSWORD=your-password")
		fmt.Println()
		fmt.Println("You can obtain credentials from:")
		fmt.Println("  https://www.epo.org/en/searching-for-patents/data/bulk-data-sets")
		os.Exit(1)
	}

	fmt.Println("Using credentials from environment")
	fmt.Printf("Username: %s\n", username)
	fmt.Println()

	// Create client
	config := bdds.DefaultConfig()
	config.Username = username
	config.Password = password

	client, err := bdds.NewClient(config)
	if err != nil {
		fmt.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Println("\n=== Main Menu ===")
		fmt.Println("1. List all products")
		fmt.Println("2. Get product details")
		fmt.Println("3. Find product by name")
		fmt.Println("4. Get latest delivery for product")
		fmt.Println("5. Download file")
		fmt.Println("q. Quit")
		fmt.Print("\nSelect option: ")

		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)

		switch choice {
		case "1":
			listProducts(ctx, client)
		case "2":
			getProductDetails(ctx, client, reader)
		case "3":
			findProductByName(ctx, client, reader)
		case "4":
			getLatestDelivery(ctx, client, reader)
		case "5":
			downloadFile(ctx, client, reader)
		case "q", "Q":
			fmt.Println("Goodbye!")
			return
		default:
			fmt.Println("Invalid option")
		}
	}
}

func listProducts(ctx context.Context, client *bdds.Client) {
	fmt.Println("\n=== Listing All Products ===")
	fmt.Println("Fetching products from EPO BDDS API...")

	products, err := client.ListProducts(ctx)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nFound %d products:\n", len(products))
	fmt.Println("----------------------------------------")

	for _, p := range products {
		fmt.Printf("ID: %d\n", p.ID)
		fmt.Printf("Name: %s\n", p.Name)
		fmt.Printf("Description: %s\n", p.Description)
		fmt.Println("----------------------------------------")
	}
}

func getProductDetails(ctx context.Context, client *bdds.Client, reader *bufio.Reader) {
	fmt.Println("\n=== Get Product Details ===")
	fmt.Print("Enter product ID (e.g., 3 for EP DocDB front file): ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	productID, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Invalid product ID: %v\n", err)
		return
	}

	fmt.Printf("\nFetching product %d...\n", productID)

	product, err := client.GetProduct(ctx, productID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("\n=== Product Information ===")
	fmt.Printf("ID: %d\n", product.ID)
	fmt.Printf("Name: %s\n", product.Name)
	fmt.Printf("Description: %s\n", product.Description)
	fmt.Printf("\nDeliveries: %d\n", len(product.Deliveries))
	fmt.Println("----------------------------------------")

	// Show first 10 deliveries
	limit := len(product.Deliveries)
	if limit > 10 {
		limit = 10
	}

	for i := 0; i < limit; i++ {
		d := product.Deliveries[i]
		fmt.Printf("\nDelivery #%d:\n", i+1)
		fmt.Printf("  ID: %d\n", d.DeliveryID)
		fmt.Printf("  Name: %s\n", d.DeliveryName)
		fmt.Printf("  Published: %s\n", d.DeliveryPublicationDatetime.Format("2006-01-02 15:04:05"))
		if d.DeliveryExpiryDatetime != nil {
			fmt.Printf("  Expires: %s\n", d.DeliveryExpiryDatetime.Format("2006-01-02 15:04:05"))
		} else {
			fmt.Printf("  Expires: Never\n")
		}
		fmt.Printf("  Files: %d\n", len(d.Files))

		// Show first 3 files
		fileLimit := len(d.Files)
		if fileLimit > 3 {
			fileLimit = 3
		}
		for j := 0; j < fileLimit; j++ {
			f := d.Files[j]
			fmt.Printf("    - %s (%s)\n", f.FileName, f.FileSize)
		}
		if len(d.Files) > 3 {
			fmt.Printf("    ... and %d more files\n", len(d.Files)-3)
		}
	}

	if len(product.Deliveries) > 10 {
		fmt.Printf("\n... and %d more deliveries\n", len(product.Deliveries)-10)
	}
}

func findProductByName(ctx context.Context, client *bdds.Client, reader *bufio.Reader) {
	fmt.Println("\n=== Find Product by Name ===")
	fmt.Print("Enter product name (e.g., 'EP DocDB front file'): ")

	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)

	if name == "" {
		fmt.Println("Product name cannot be empty")
		return
	}

	fmt.Printf("\nSearching for product '%s'...\n", name)

	product, err := client.GetProductByName(ctx, name)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("\n=== Product Found ===")
	fmt.Printf("ID: %d\n", product.ID)
	fmt.Printf("Name: %s\n", product.Name)
	fmt.Printf("Description: %s\n", product.Description)
}

func getLatestDelivery(ctx context.Context, client *bdds.Client, reader *bufio.Reader) {
	fmt.Println("\n=== Get Latest Delivery ===")
	fmt.Print("Enter product ID (e.g., 3): ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	productID, err := strconv.Atoi(input)
	if err != nil {
		fmt.Printf("Invalid product ID: %v\n", err)
		return
	}

	fmt.Printf("\nFetching latest delivery for product %d...\n", productID)

	delivery, err := client.GetLatestDelivery(ctx, productID)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("\n=== Latest Delivery ===")
	fmt.Printf("ID: %d\n", delivery.DeliveryID)
	fmt.Printf("Name: %s\n", delivery.DeliveryName)
	fmt.Printf("Published: %s\n", delivery.DeliveryPublicationDatetime.Format("2006-01-02 15:04:05"))
	if delivery.DeliveryExpiryDatetime != nil {
		fmt.Printf("Expires: %s\n", delivery.DeliveryExpiryDatetime.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Printf("Expires: Never\n")
	}
	fmt.Printf("\nFiles in this delivery: %d\n", len(delivery.Files))
	fmt.Println("----------------------------------------")

	// Show all files
	for i, f := range delivery.Files {
		fmt.Printf("%d. %s\n", i+1, f.FileName)
		fmt.Printf("   Size: %s\n", f.FileSize)
		fmt.Printf("   Checksum: %s\n", f.FileChecksum)
		fmt.Printf("   Published: %s\n", f.FilePublicationDatetime.Format("2006-01-02 15:04:05"))
		fmt.Println()
	}
}

func downloadFile(ctx context.Context, client *bdds.Client, reader *bufio.Reader) {
	fmt.Println("\n=== Download File ===")
	fmt.Print("Enter product ID: ")
	productInput, _ := reader.ReadString('\n')
	productInput = strings.TrimSpace(productInput)

	productID, err := strconv.Atoi(productInput)
	if err != nil {
		fmt.Printf("Invalid product ID: %v\n", err)
		return
	}

	fmt.Print("Enter delivery ID: ")
	deliveryInput, _ := reader.ReadString('\n')
	deliveryInput = strings.TrimSpace(deliveryInput)

	deliveryID, err := strconv.Atoi(deliveryInput)
	if err != nil {
		fmt.Printf("Invalid delivery ID: %v\n", err)
		return
	}

	fmt.Print("Enter file ID: ")
	fileInput, _ := reader.ReadString('\n')
	fileInput = strings.TrimSpace(fileInput)

	fileID, err := strconv.Atoi(fileInput)
	if err != nil {
		fmt.Printf("Invalid file ID: %v\n", err)
		return
	}

	fmt.Print("Enter output filename (e.g., download.zip): ")
	filename, _ := reader.ReadString('\n')
	filename = strings.TrimSpace(filename)

	if filename == "" {
		filename = "download.zip"
	}

	fmt.Printf("\nDownloading file...\n")
	fmt.Printf("Product ID: %d\n", productID)
	fmt.Printf("Delivery ID: %d\n", deliveryID)
	fmt.Printf("File ID: %d\n", fileID)
	fmt.Printf("Output: %s\n\n", filename)

	file, err := os.Create(filename)
	if err != nil {
		fmt.Printf("Error creating file: %v\n", err)
		return
	}
	defer file.Close()

	startTime := time.Now()
	var lastUpdate time.Time

	err = client.DownloadFileWithProgress(ctx, productID, deliveryID, fileID, file,
		func(bytesWritten, totalBytes int64) {
			now := time.Now()
			// Update every 500ms
			if now.Sub(lastUpdate) > 500*time.Millisecond {
				if totalBytes > 0 {
					percent := float64(bytesWritten) * 100 / float64(totalBytes)
					elapsed := now.Sub(startTime).Seconds()
					speed := float64(bytesWritten) / elapsed / 1024 / 1024 // MB/s

					fmt.Printf("\rProgress: %.1f%% | %.2f/%.2f MB | Speed: %.2f MB/s     ",
						percent,
						float64(bytesWritten)/1024/1024,
						float64(totalBytes)/1024/1024,
						speed)
				} else {
					fmt.Printf("\rDownloaded: %.2f MB     ", float64(bytesWritten)/1024/1024)
				}
				lastUpdate = now
			}
		})

	fmt.Println() // New line after progress

	if err != nil {
		fmt.Printf("Error downloading file: %v\n", err)
		os.Remove(filename)
		return
	}

	info, _ := file.Stat()
	elapsed := time.Since(startTime)
	avgSpeed := float64(info.Size()) / elapsed.Seconds() / 1024 / 1024

	fmt.Printf("\nSuccess! Downloaded to: %s\n", filename)
	fmt.Printf("Size: %.2f MB\n", float64(info.Size())/1024/1024)
	fmt.Printf("Time: %.1f seconds\n", elapsed.Seconds())
	fmt.Printf("Avg Speed: %.2f MB/s\n", avgSpeed)
}
