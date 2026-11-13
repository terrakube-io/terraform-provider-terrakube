package provider

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const notificationTestSigningSecret = "s3cr3t-signing"

type notificationConfigurationTestCase struct {
	name          string
	newResource   func(server *httptest.Server) resource.Resource
	schemaAndType func(t *testing.T, ctx context.Context) (schema.Schema, tftypes.Object)
	extra         map[string]tftypes.Value
}

func notificationConfigurationTestCases() []notificationConfigurationTestCase {
	return []notificationConfigurationTestCase{
		{
			name: "organization",
			newResource: func(server *httptest.Server) resource.Resource {
				if server == nil {
					return &OrganizationNotificationConfigurationResource{}
				}
				return &OrganizationNotificationConfigurationResource{client: server.Client(), endpoint: server.URL, token: "test-token"}
			},
			schemaAndType: organizationNotificationConfigurationSchemaAndType,
		},
		{
			name: "workspace",
			newResource: func(server *httptest.Server) resource.Resource {
				if server == nil {
					return &WorkspaceNotificationConfigurationResource{}
				}
				return &WorkspaceNotificationConfigurationResource{client: server.Client(), endpoint: server.URL, token: "test-token"}
			},
			schemaAndType: workspaceNotificationConfigurationSchemaAndType,
			extra: map[string]tftypes.Value{
				"workspace_id": tftypes.NewValue(tftypes.String, "ws-1"),
			},
		},
	}
}

func (tc notificationConfigurationTestCase) values(objType tftypes.Object, overrides map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "signed-webhook"),
		"channel_type":    tftypes.NewValue(tftypes.String, "WEBHOOK"),
		"destination_url": tftypes.NewValue(tftypes.String, "https://example.com/hook"),
		"signing_secret":  tftypes.NewValue(tftypes.String, notificationTestSigningSecret),
		"active":          tftypes.NewValue(tftypes.Bool, true),
		"message_style":   tftypes.NewValue(tftypes.String, "DETAILED"),
		"trigger_statuses": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "failed"),
		}),
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{}),
	}
	maps.Copy(values, tc.extra)
	maps.Copy(values, overrides)
	return buildObjectValue(objType, values)
}

// notificationConfigurationServer answers as Terrakube does: signingSecret is
// never in a response (its "read notification signing secret" check always
// denies), and the configuration has the "failed" trigger and no templates. When captured
// is not nil, it records the body of the last configuration POST or PATCH.
func notificationConfigurationServer(t *testing.T, captured *[]byte) *httptest.Server {
	t.Helper()

	config := `{"data":{"type":"notification_configuration","id":"cfg-1","attributes":{` +
		`"name":"signed-webhook","channelType":"WEBHOOK","destinationUrl":"https://example.com/hook","active":true,"messageStyle":"DETAILED"}}}`
	triggers := `{"data":[{"type":"notification_trigger","id":"trg-1","attributes":{"jobStatus":"failed"}}]}`

	mux := http.NewServeMux()
	handler := func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		switch {
		case strings.HasSuffix(req.URL.Path, "/notificationConfiguration"),
			req.URL.Path == "/api/v1/notification_configuration/cfg-1":
			if captured != nil && req.Method != http.MethodGet {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("reading request body: %v", err)
				}
				*captured = body
			}
			fmt.Fprint(w, config)
		case strings.HasSuffix(req.URL.Path, "/triggers"):
			if req.Method == http.MethodPost {
				w.WriteHeader(http.StatusCreated)
				return
			}
			fmt.Fprint(w, triggers)
		case strings.HasSuffix(req.URL.Path, "/templates"):
			if req.Method == http.MethodPatch {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			fmt.Fprint(w, `{"data":[]}`)
		default:
			t.Errorf("unexpected request %s %s", req.Method, req.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}
	mux.HandleFunc("/api/v1/", handler)

	return httptest.NewServer(mux)
}

func signingSecretFromState(t *testing.T, ctx context.Context, state tfsdk.State) types.String {
	t.Helper()

	var secret types.String
	if diags := state.GetAttribute(ctx, path.Root("signing_secret"), &secret); diags.HasError() {
		t.Fatalf("reading signing_secret from state: %v", diags)
	}
	return secret
}

// Terrakube never returns signingSecret, so Create must keep the planned value;
// copying the (empty) response made Terraform fail with "Provider produced
// inconsistent result after apply".
func TestNotificationConfigurationResource_Create_KeepsSigningSecret(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)
			server := notificationConfigurationServer(t, nil)
			defer server.Close()

			plan := tc.values(objType, nil)
			req := resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}, Config: tfsdk.Config{Schema: s, Raw: plan}}
			resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}

			tc.newResource(server).Create(ctx, req, resp)

			if resp.Diagnostics.HasError() {
				t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
			}
			if got := signingSecretFromState(t, ctx, resp.State); got.ValueString() != notificationTestSigningSecret {
				t.Errorf("expected signing_secret to stay as planned, got: %v", got)
			}
		})
	}
}

// Read must keep the signing secret from the state; otherwise every refresh
// would clear it and the next plan would show a change.
func TestNotificationConfigurationResource_Read_KeepsSigningSecret(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)
			server := notificationConfigurationServer(t, nil)
			defer server.Close()

			for name, secret := range map[string]tftypes.Value{
				"set":   tftypes.NewValue(tftypes.String, notificationTestSigningSecret),
				"unset": tftypes.NewValue(tftypes.String, nil),
			} {
				t.Run(name, func(t *testing.T) {
					stateValue := tc.values(objType, map[string]tftypes.Value{
						"id":             tftypes.NewValue(tftypes.String, "cfg-1"),
						"signing_secret": secret,
					})
					req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
					resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}

					tc.newResource(server).Read(ctx, req, resp)

					if resp.Diagnostics.HasError() {
						t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
					}
					got := signingSecretFromState(t, ctx, resp.State)
					if secret.IsNull() {
						if !got.IsNull() {
							t.Errorf("expected signing_secret to stay null, got: %v", got)
						}
						return
					}
					if got.ValueString() != notificationTestSigningSecret {
						t.Errorf("expected signing_secret to be kept from the state, got: %v", got)
					}
				})
			}
		})
	}
}
