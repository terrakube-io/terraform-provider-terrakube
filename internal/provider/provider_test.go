package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestNewTerrakubeClientSendsUserAgentAndToken(t *testing.T) {
	var gotAgent, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent = r.Header.Get("User-Agent")
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	client, err := newTerrakubeClient(server.URL, "test-token", false, "1.2.3")
	if err != nil {
		t.Fatalf("newTerrakubeClient: %v", err)
	}
	if _, err := client.Organizations.List(context.Background(), nil); err != nil {
		t.Fatalf("list organizations: %v", err)
	}

	if gotAgent != "terraform-provider-terrakube/1.2.3" {
		t.Errorf("User-Agent = %q, want %q", gotAgent, "terraform-provider-terrakube/1.2.3")
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-token")
	}
}

func TestNewTerrakubeClientTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	tests := []struct {
		name     string
		insecure bool
		wantErr  bool
	}{
		{name: "insecure accepts a self-signed certificate", insecure: true},
		{name: "default rejects a self-signed certificate", insecure: false, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newTerrakubeClient(server.URL, "test-token", tt.insecure, "test")
			if err != nil {
				t.Fatalf("newTerrakubeClient: %v", err)
			}
			_, err = client.Organizations.List(context.Background(), nil)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "certificate") {
					t.Fatalf("want a certificate error, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("list organizations: %v", err)
			}
		})
	}
}

func configureProvider(t *testing.T, endpoint, token tftypes.Value) *provider.ConfigureResponse {
	t.Helper()
	t.Setenv("TERRAKUBE_ENDPOINT", "")
	t.Setenv("TERRAKUBE_TOKEN", "")

	p := New("test")()
	schemaResp := &provider.SchemaResponse{}
	p.Schema(context.Background(), provider.SchemaRequest{}, schemaResp)

	objType := tftypes.Object{AttributeTypes: map[string]tftypes.Type{
		"endpoint":             tftypes.String,
		"token":                tftypes.String,
		"insecure_http_client": tftypes.Bool,
	}}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"endpoint":             endpoint,
		"token":                token,
		"insecure_http_client": tftypes.NewValue(tftypes.Bool, nil),
	})

	resp := &provider.ConfigureResponse{}
	p.Configure(context.Background(), provider.ConfigureRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: raw},
	}, resp)
	return resp
}

func TestProviderConfigureSharesOneClient(t *testing.T) {
	resp := configureProvider(t,
		tftypes.NewValue(tftypes.String, "https://terrakube.example.com"),
		tftypes.NewValue(tftypes.String, "test-token"))

	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	resourceData, ok := resp.ResourceData.(*TerrakubeConnectionData)
	if !ok || resourceData.Client == nil {
		t.Fatalf("ResourceData = %#v, want *TerrakubeConnectionData with a client", resp.ResourceData)
	}
	dataSourceData, ok := resp.DataSourceData.(*TerrakubeConnectionData)
	if !ok || dataSourceData.Client != resourceData.Client {
		t.Fatalf("DataSourceData does not share the resource client")
	}
}

func TestProviderConfigureRejectsMissingOrUnknownSettings(t *testing.T) {
	tests := []struct {
		name     string
		endpoint tftypes.Value
		token    tftypes.Value
		want     string
	}{
		{
			name:     "missing endpoint",
			endpoint: tftypes.NewValue(tftypes.String, nil),
			token:    tftypes.NewValue(tftypes.String, "test-token"),
			want:     "Missing Terrakube API Host",
		},
		{
			name:     "missing token",
			endpoint: tftypes.NewValue(tftypes.String, "https://terrakube.example.com"),
			token:    tftypes.NewValue(tftypes.String, nil),
			want:     "Missing Terrakube API Token",
		},
		{
			name:     "unknown token",
			endpoint: tftypes.NewValue(tftypes.String, "https://terrakube.example.com"),
			token:    tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
			want:     "Unknown Terrakube API token",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := configureProvider(t, tt.endpoint, tt.token)
			if !resp.Diagnostics.HasError() {
				t.Fatal("want an error diagnostic")
			}
			if got := resp.Diagnostics.Errors()[0].Summary(); got != tt.want {
				t.Errorf("summary = %q, want %q", got, tt.want)
			}
			if resp.ResourceData != nil || resp.DataSourceData != nil {
				t.Error("provider data must not be set when configuration fails")
			}
		})
	}
}
