package provider

import (
	"net/http"

	terrakube "github.com/terrakube-io/terrakube-go"
)

// newTestClient builds a terrakube-go client that sends its requests through c to endpoint,
// usually an httptest server, with the token test-token.
func newTestClient(c *http.Client, endpoint string) *terrakube.Client {
	client, err := terrakube.NewClient(
		terrakube.WithHTTPClient(c),
		terrakube.WithEndpoint(endpoint),
		terrakube.WithToken("test-token"),
	)
	if err != nil {
		panic(err)
	}
	return client
}
