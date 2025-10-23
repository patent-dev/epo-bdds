package bdds

import "time"

// Product represents a BDDS product
type Product struct {
	ID          int
	Name        string
	Description string
}

// ProductWithDeliveries represents a product with its deliveries
type ProductWithDeliveries struct {
	ID          int
	Name        string
	Description string
	Deliveries  []*Delivery
}

// Delivery represents a product delivery
type Delivery struct {
	DeliveryID                  int
	DeliveryName                string
	DeliveryPublicationDatetime time.Time
	DeliveryExpiryDatetime      *time.Time
	Files                       []*DeliveryFile
}

// DeliveryFile represents a file in a delivery
type DeliveryFile struct {
	FileID                  int
	FileName                string
	FileSize                string
	FileChecksum            string
	FilePublicationDatetime time.Time
}
