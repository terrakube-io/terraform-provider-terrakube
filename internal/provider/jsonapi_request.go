package provider

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/attr"
)

// newTerrakubeHTTPClient builds the HTTP client for the connection settings of the provider.
func newTerrakubeHTTPClient(data *TerrakubeConnectionData) *http.Client {
	if data.InsecureHttpClient {
		if custom, ok := http.DefaultTransport.(*http.Transport); ok {
			customTransport := custom.Clone()
			customTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
			return &http.Client{Transport: customTransport}
		}
	}
	return &http.Client{}
}

// doJSONAPIRequest sends a request to the Terrakube API and returns the response with its body
// read and closed. A nil body sends no body.
func doJSONAPIRequest(ctx context.Context, c *http.Client, token, method, url string, body []byte) (*http.Response, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, nil, fmt.Errorf("creating request: %w", err)
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := c.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("executing request: %w", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response, nil, fmt.Errorf("reading response: %w", err)
	}
	return response, responseBody, nil
}

// attrValue lets a list of framework values be built from a slice of any value type.
type attrValue = attr.Value

// protocolPattern is a plugin protocol version such as 5.0 or 6.0.
var protocolPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

// marshalJSONAPI renders an entity as a JSON:API document.
func marshalJSONAPI(entity any) ([]byte, error) {
	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, entity); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
