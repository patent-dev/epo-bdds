# EPO BDDS API Client Demo

Interactive demo application showcasing all features of the EPO BDDS Go client library.

## Prerequisites

This demo showcases both authenticated and unauthenticated features. Credentials are optional:
- **Information**: https://www.epo.org/en/searching-for-patents/data/bulk-data-sets
- **Note**: Free products work without credentials; paid products require EPO BDDS subscription

## Running the Demo

### 1. Set your credentials

```bash
export EPO_BDDS_USERNAME=your-username
export EPO_BDDS_PASSWORD=your-password
```

### 2. Run the demo

```bash
cd demo
go run demo.go
```

## Features Demonstrated

### 1. List All Products
Browse all available BDDS products (both subscribed and public).

### 2. Get Product Details
View detailed information about a specific product including:
- All available deliveries
- Delivery publication and expiry dates
- Files in each delivery
- File sizes and checksums

### 3. Find Product by Name
Search for a product by name (case-insensitive).

### 4. Get Latest Delivery
Fetch the most recent delivery for a product with all file details.

### 5. Download File
Download a file from a delivery with:
- Real-time progress bar
- Download speed monitoring
- Automatic checksum verification (if implemented)

## Example Session

```
EPO BDDS API Client Demo
=========================

Using credentials from environment
Username: your-username

=== Main Menu ===
1. List all products
2. Get product details
3. Find product by name
4. Get latest delivery for product
5. Download file
q. Quit

Select option: 1

=== Listing All Products ===
Fetching products from EPO BDDS API...

Found 5 products:
----------------------------------------
ID: 3
Name: EP DocDB front file
Description: EP DocDB front file - bibliographic data
----------------------------------------
ID: 4
Name: EP full-text data - front file
Description: Full-text patent data - front file
----------------------------------------
...

Select option: 2

=== Get Product Details ===
Enter product ID (e.g., 3 for EP DocDB front file): 3

Fetching product 3...

=== Product Information ===
ID: 3
Name: EP DocDB front file
Description: EP DocDB front file - bibliographic data

Deliveries: 52
----------------------------------------

Delivery #1:
  ID: 12345
  Name: 2024-10-15
  Published: 2024-10-15 10:30:00
  Expires: 2024-11-15 10:30:00
  Files: 3
    - EP_docdb_20241015.zip (1.5 GB)
    - EP_docdb_20241015_checksum.txt (256 bytes)
    - EP_docdb_20241015_readme.txt (1.2 KB)
...
```

## Common Product IDs

| ID | Name | Description |
|----|------|-------------|
| 3  | EP DocDB front file | Bibliographic data (front file) |
| 4  | EP full-text data - front file | Full-text patent data |
| 14 | EP DocDB back file | Bibliographic data (back file) |
| 17 | PATSTAT Global | Patent statistics database |
| 18 | PATSTAT EP Register | EP register data |

## Authentication

The demo automatically handles OAuth2 authentication:
- Tokens are obtained on first API call
- Tokens are cached and automatically refreshed before expiry
- Re-authentication happens automatically on 401 errors

You don't need to manually manage tokens!

## Error Handling

The demo demonstrates proper error handling:
- Authentication failures
- Network errors
- Invalid product/delivery/file IDs
- Rate limiting (if encountered)

## Download Tips

- File downloads can be very large (multiple GB)
- Progress is shown in real-time with speed monitoring
- Downloads can be interrupted with Ctrl+C
- Downloaded files are saved to the current directory

## Notes

- This demo uses the production EPO BDDS API
- Some products may require specific subscription levels
- Download speeds depend on your internet connection
- Large file downloads may take significant time

## Troubleshooting

### Authentication Failed
```
Error: authentication failed (status 401): invalid credentials
```
**Solution**: Verify your EPO_BDDS_USERNAME and EPO_BDDS_PASSWORD are correct.

### Product Not Found
```
Error: product not found: 999
```
**Solution**: Use option 1 to list all available products and their IDs.

### File Download Fails
```
Error: file not found: 3/12345/67890
```
**Solution**: Use option 2 or 4 to verify the delivery ID and file ID exist.

## Building

To build a standalone executable:

```bash
cd demo
go build -o epo-bdds-demo demo.go
./epo-bdds-demo
```

## License

MIT License - see LICENSE file for details.
