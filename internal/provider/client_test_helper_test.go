package provider

import (
	"net/http"

	terrakube "github.com/terrakube-io/terrakube-go"
)

// newTestClient builds a terrakube-go client that sends its requests through c to endpoint,
// usually an httptest server, with the token test-token. An empty endpoint, for a test that sends
// no request, gives a nil client.
func newTestClient(c *http.Client, endpoint string) *terrakube.Client {
	if endpoint == "" {
		return nil
	}
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
