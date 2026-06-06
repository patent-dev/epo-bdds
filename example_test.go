package bdds_test

import (
	"fmt"

	bdds "github.com/patent-dev/epo-bdds"
)

func ExampleNewClient() {
	// Credentials are optional: free/public products work without them,
	// paid products require an EPO BDDS subscription.
	client, err := bdds.NewClient(&bdds.Config{
		Username: "your-epo-username",
		Password: "your-epo-password",
	})
	if err != nil {
		panic(err)
	}
	// client is ready; call ListProducts, GetProduct, DownloadFile, etc.
	fmt.Println(client != nil)
	// Output: true
}
