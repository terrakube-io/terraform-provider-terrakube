package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	terrakube "github.com/terrakube-io/terrakube-go"
)

func TestSyncNotificationTriggers_AddsAndRemoves(t *testing.T) {
	ctx := context.Background()
	var posted []string
	var deleted []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method %s on triggers collection", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		posted = append(posted, string(body))
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers/trig-old", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("unexpected method %s on trigger item", r.Method)
		}
		deleted = append(deleted, "trig-old")
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.Client(), server.URL)

	current := []*terrakube.NotificationTrigger{
		{ID: "trig-old", JobStatus: "completed"},
	}
	diags := syncNotificationTriggers(ctx, c, "cfg-1", current, []string{"failed"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if len(posted) != 1 {
		t.Fatalf("expected 1 POST for the new trigger, got %d", len(posted))
	}
	if wantSubstr := `"jobStatus":"failed"`; !contains(posted[0], wantSubstr) {
		t.Errorf("expected trigger POST body to contain %s, got: %s", wantSubstr, posted[0])
	}
	if len(deleted) != 1 || deleted[0] != "trig-old" {
		t.Errorf("expected DELETE of trig-old, got %v", deleted)
	}
}

func TestSyncNotificationTriggers_NoChangesMakesNoRequests(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected request %s %s when no trigger changes were needed", r.Method, r.URL.Path)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	c := newTestClient(server.Client(), server.URL)

	current := []*terrakube.NotificationTrigger{{ID: "trig-1", JobStatus: "failed"}}
	diags := syncNotificationTriggers(ctx, c, "cfg-1", current, []string{"failed"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

// readNotificationConfigurationTemplateIDs runs the data source's Read against a configuration
// list whose match has the given templates relationship, and returns template_ids from the state.
func readNotificationConfigurationTemplateIDs(t *testing.T, templatesLinkage string) []string {
	t.Helper()
	ctx := context.Background()
	s, objType := notificationConfigurationDataSourceSchemaAndType(t, ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/organization/org-1/notificationConfiguration", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		fmt.Fprintf(w, `{"data":[{"type":"notification_configuration","id":"cfg-1","attributes":{
			"name":"prod-alerts","channelType":"SLACK","destinationUrl":"https://org","active":true,"messageStyle":"DETAILED"},
			"relationships":{"templates":{"data":%s}}}]}`, templatesLinkage)
	})
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	d := &NotificationConfigurationDataSource{client: newTestClient(server.Client(), server.URL)}
	configValue := buildObjectValue(objType, map[string]tftypes.Value{
		"organization_id": tftypes.NewValue(tftypes.String, "org-1"),
		"name":            tftypes.NewValue(tftypes.String, "prod-alerts"),
	})
	resp := &datasource.ReadResponse{State: tfsdk.State{Schema: s}}
	d.Read(ctx, datasource.ReadRequest{Config: tfsdk.Config{Schema: s, Raw: configValue}}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}

	var ids []string
	if diags := resp.State.GetAttribute(ctx, path.Root("template_ids"), &ids); diags.HasError() {
		t.Fatalf("reading template_ids from state: %v", diags)
	}
	return ids
}

// The data source takes template_ids from the templates relationship linkage of the listed
// configuration (notificationTemplateIDs): the ids only, in order.
func TestNotificationTemplateIDs_ReturnsIDsOnly(t *testing.T) {
	ids := readNotificationConfigurationTemplateIDs(t, `[{"type":"template","id":"tmpl-1"},{"type":"template","id":"tmpl-2"}]`)
	if len(ids) != 2 || ids[0] != "tmpl-1" || ids[1] != "tmpl-2" {
		t.Errorf("ids = %v, want [tmpl-1 tmpl-2]", ids)
	}
}

func TestNotificationTemplateIDs_EmptyMeansAllTemplates(t *testing.T) {
	ids := readNotificationConfigurationTemplateIDs(t, `[]`)
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty (meaning \"applies to every template\")", ids)
	}
}

// updateTemplatesPatchBody runs Update with the given template_ids in the plan (and tmpl-old in
// the state) and returns the configuration PATCH body Terrakube would have received.
func updateTemplatesPatchBody(t *testing.T, tc notificationConfigurationTestCase, templateIDs []string) []byte {
	t.Helper()
	ctx := context.Background()
	s, objType := tc.schemaAndType(t, ctx)
	id := tftypes.NewValue(tftypes.String, "cfg-1")

	planned := make([]tftypes.Value, 0, len(templateIDs))
	for _, templateID := range templateIDs {
		planned = append(planned, tftypes.NewValue(tftypes.String, templateID))
	}
	state := tc.values(objType, map[string]tftypes.Value{
		"id": id,
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{
			tftypes.NewValue(tftypes.String, "tmpl-old"),
		}),
	})
	plan := tc.values(objType, map[string]tftypes.Value{
		"id":           id,
		"template_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, planned),
	})

	var captured []byte
	server := notificationConfigurationServer(t, &captured)
	defer server.Close()

	req := resource.UpdateRequest{
		State:  tfsdk.State{Schema: s, Raw: state},
		Plan:   tfsdk.Plan{Schema: s, Raw: plan},
		Config: tfsdk.Config{Schema: s, Raw: plan},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s}}
	tc.newResource(server).Update(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
	}
	return captured
}

// sentTemplateIDs returns the ids in the templates relationship of a JSON:API request body, and
// whether the body has that relationship. Every linkage must have the type template.
func sentTemplateIDs(t *testing.T, body []byte) ([]string, bool) {
	t.Helper()
	var document struct {
		Data struct {
			Relationships map[string]struct {
				Data []struct {
					Type string `json:"type"`
					ID   string `json:"id"`
				} `json:"data"`
			} `json:"relationships"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &document); err != nil {
		t.Fatalf("request body is not JSON: %v: %s", err, body)
	}
	templates, ok := document.Data.Relationships["templates"]
	if !ok {
		return nil, false
	}
	ids := make([]string, 0, len(templates.Data))
	for _, linkage := range templates.Data {
		if linkage.Type != "template" {
			t.Errorf("templates linkage type = %q, want template", linkage.Type)
		}
		ids = append(ids, linkage.ID)
	}
	return ids, true
}

// The configuration PATCH replaces the whole templates set with the planned one.
func TestNotificationConfigurationResource_Update_SendsFullTemplatesReplace(t *testing.T) {
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := updateTemplatesPatchBody(t, tc, []string{"tmpl-1", "tmpl-2"})

			for _, wantSubstr := range []string{`"type":"template"`, `"id":"tmpl-1"`, `"id":"tmpl-2"`} {
				if !contains(string(body), wantSubstr) {
					t.Errorf("expected PATCH body to contain %s, got: %s", wantSubstr, body)
				}
			}
			if ids, ok := sentTemplateIDs(t, body); !ok || !slices.Equal(ids, []string{"tmpl-1", "tmpl-2"}) {
				t.Errorf("expected the templates relationship to be [tmpl-1 tmpl-2], got: %s", body)
			}
		})
	}
}

// An empty template_ids clears the set: the relationship is sent with no linkage, not left out.
func TestNotificationConfigurationResource_Update_EmptyTemplatesClearsTheSet(t *testing.T) {
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			body := updateTemplatesPatchBody(t, tc, []string{})

			if wantSubstr := `"templates":{"data":[]}`; !contains(string(body), wantSubstr) {
				t.Errorf("expected PATCH body to contain %s (clearing the set), got: %s", wantSubstr, body)
			}
		})
	}
}

// Read takes template_ids from the templates relationship linkage of the configuration's GET
// response, in its order.
func TestNotificationConfigurationResource_Read_TemplateIDsFromLinkage(t *testing.T) {
	ctx := context.Background()
	for _, tc := range notificationConfigurationTestCases() {
		t.Run(tc.name, func(t *testing.T) {
			s, objType := tc.schemaAndType(t, ctx)

			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/notification_configuration/cfg-1", func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("unexpected method %s", r.Method)
				}
				fmt.Fprint(w, `{"data":{"type":"notification_configuration","id":"cfg-1","attributes":{
					"name":"signed-webhook","channelType":"WEBHOOK","destinationUrl":"https://example.com/hook","active":true,"messageStyle":"DETAILED"},
					"relationships":{"templates":{"data":[{"type":"template","id":"tmpl-2"},{"type":"template","id":"tmpl-1"}]}}}}`)
			})
			mux.HandleFunc("/api/v1/notification_configuration/cfg-1/triggers", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"data":[{"type":"notification_trigger","id":"trg-1","attributes":{"jobStatus":"failed"}}]}`)
			})
			server := httptest.NewServer(mux)
			defer server.Close()

			stateValue := tc.values(objType, map[string]tftypes.Value{"id": tftypes.NewValue(tftypes.String, "cfg-1")})
			req := resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: stateValue}}
			resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: stateValue}}
			tc.newResource(server).Read(ctx, req, resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
			}

			var ids []string
			if diags := resp.State.GetAttribute(ctx, path.Root("template_ids"), &ids); diags.HasError() {
				t.Fatalf("reading template_ids from state: %v", diags)
			}
			if !slices.Equal(ids, []string{"tmpl-2", "tmpl-1"}) {
				t.Errorf("template_ids = %v, want [tmpl-2 tmpl-1]", ids)
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || len(needle) == 0 || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
