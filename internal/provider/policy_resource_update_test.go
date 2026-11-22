package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// policyTestServer answers a PATCH with 204, as Terrakube does, and every other request with
// response. sent returns the JSON:API resource object ("data") of the last request with a method.
func policyTestServer(t *testing.T, response string) (server *httptest.Server, sent func(method string) map[string]any) {
	t.Helper()
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Errorf("request body is not JSON: %v: %s", err, raw)
			}
			data, _ := body["data"].(map[string]any)
			mu.Lock()
			bodies[r.Method] = data
			mu.Unlock()
		}
		if r.Method == http.MethodPatch {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_, _ = io.WriteString(w, response)
	}))
	t.Cleanup(server.Close)
	return server, func(method string) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		data, ok := bodies[method]
		if !ok {
			t.Fatalf("no %s request was sent", method)
		}
		return data
	}
}

func sentAttributes(data map[string]any) map[string]any {
	attributes, _ := data["attributes"].(map[string]any)
	return attributes
}

// policyStateString returns an attribute of the state, "<null>" for a null.
func policyStateString(t *testing.T, state tfsdk.State, name string) string {
	t.Helper()
	var values map[string]tftypes.Value
	if err := state.Raw.As(&values); err != nil {
		t.Fatalf("reading the state: %v", err)
	}
	if values[name].IsNull() {
		return "<null>"
	}
	var text string
	if err := values[name].As(&text); err != nil {
		t.Fatalf("attribute %s is not a string: %v", name, err)
	}
	return text
}

func runUpdate(t *testing.T, r resource.Resource, prior, plan tftypes.Value) tfsdk.State {
	t.Helper()
	ctx := context.Background()
	var schemaResp resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
	s := schemaResp.Schema
	req := resource.UpdateRequest{
		State: tfsdk.State{Schema: s, Raw: prior},
		Plan:  tfsdk.Plan{Schema: s, Raw: plan},
	}
	resp := &resource.UpdateResponse{State: tfsdk.State{Schema: s, Raw: prior}}
	r.Update(ctx, req, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	return resp.State
}

func TestPolicySetResource_UpdateKeepsRemovedLinksOutOfState(t *testing.T) {
	_, objType := policySetResourceSchemaAndType(t, context.Background())
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

	// Terrakube keeps the links, since the PATCH leaves them out.
	server, sent := policyTestServer(t, `{"data":{"type":"policy_set","id":"ps-1","attributes":{
		"name":"guardrails","enforcementLevel":"HARD_MANDATORY","global":false,"branch":"main","folder":"/"
	},"relationships":{
		"vcs":{"data":{"type":"vcs","id":"vcs-1"}},
		"notificationConfiguration":{"data":{"type":"notification_configuration","id":"nc-1"}}
	}}}`)
	common := map[string]tftypes.Value{
		"id": str("ps-1"), "organization_id": str("org-1"), "name": str("guardrails"),
		"enforcement_level": str("hard_mandatory"), "global": tftypes.NewValue(tftypes.Bool, false),
		"branch": str("main"), "folder": str("/"),
	}
	prior := map[string]tftypes.Value{"vcs_id": str("vcs-1"), "notification_configuration_id": str("nc-1")}
	for k, v := range common {
		prior[k] = v
	}

	state := runUpdate(t, &PolicySetResource{client: newTestClient(server.Client(), server.URL)},
		buildObjectValue(objType, prior), buildObjectValue(objType, common))

	if _, ok := sent(http.MethodPatch)["relationships"]; ok {
		t.Errorf("PATCH sent relationships, want none: %v", sent(http.MethodPatch))
	}
	for _, name := range []string{"vcs_id", "notification_configuration_id"} {
		if got := policyStateString(t, state, name); got != "<null>" {
			t.Errorf("%s = %q, want null as planned", name, got)
		}
	}
}

func TestPolicyExemptionResource_UpdateKeepsRemovedLinksOutOfState(t *testing.T) {
	_, objType := policyExemptionResourceSchemaAndType(t, context.Background())
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

	server, sent := policyTestServer(t, `{"data":{"type":"policy_exemption","id":"ex-1","attributes":{
		"ruleId":"rule","ticketReference":"SEC-1","justification":"why"
	},"relationships":{
		"policySet":{"data":{"type":"policy_set","id":"ps-1"}},
		"workspace":{"data":{"type":"workspace","id":"ws-1"}},
		"project":{"data":{"type":"project","id":"proj-1"}}
	}}}`)
	common := map[string]tftypes.Value{
		"id": str("ex-1"), "organization_id": str("org-1"), "policy_set_id": str("ps-1"),
		"rule_id": str("rule"), "ticket_reference": str("SEC-1"), "justification": str("why"),
	}
	prior := map[string]tftypes.Value{"workspace_id": str("ws-1"), "project_id": str("proj-1")}
	for k, v := range common {
		prior[k] = v
	}

	state := runUpdate(t, &PolicyExemptionResource{client: newTestClient(server.Client(), server.URL)},
		buildObjectValue(objType, prior), buildObjectValue(objType, common))

	relationships, _ := sent(http.MethodPatch)["relationships"].(map[string]any)
	if _, ok := relationships["workspace"]; ok {
		t.Errorf("PATCH sent a workspace link: %v", relationships)
	}
	if _, ok := relationships["project"]; ok {
		t.Errorf("PATCH sent a project link: %v", relationships)
	}
	for _, name := range []string{"workspace_id", "project_id"} {
		if got := policyStateString(t, state, name); got != "<null>" {
			t.Errorf("%s = %q, want null as planned", name, got)
		}
	}
	if got := policyStateString(t, state, "policy_set_id"); got != "ps-1" {
		t.Errorf("policy_set_id = %q, want ps-1", got)
	}
}

func TestPolicySetParameterResource_UpdateClearsRemovedDescription(t *testing.T) {
	_, objType := policySetParameterResourceSchemaAndType(t, context.Background())
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }

	// The read-back after the 204 shows the description is gone.
	server, sent := policyTestServer(t, `{"data":{"type":"policy_set_parameter","id":"param-1","attributes":{
		"key":"max_vcpus","value":"64"
	}}}`)
	common := map[string]tftypes.Value{
		"id": str("param-1"), "policy_set_id": str("ps-1"), "key": str("max_vcpus"), "value": str("64"),
	}
	prior := map[string]tftypes.Value{"description": str("Max allowed vCPUs")}
	for k, v := range common {
		prior[k] = v
	}

	state := runUpdate(t, &PolicySetParameterResource{client: newTestClient(server.Client(), server.URL)},
		buildObjectValue(objType, prior), buildObjectValue(objType, common))

	attributes := sentAttributes(sent(http.MethodPatch))
	if description, ok := attributes["description"]; !ok || description != nil {
		t.Errorf("PATCH attributes = %v, want description null", attributes)
	}
	if got := policyStateString(t, state, "description"); got != "<null>" {
		t.Errorf("description = %q, want null", got)
	}
}

func TestPolicySetResource_EnforcementLevelCase(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetResourceSchemaAndType(t, ctx)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	const response = `{"data":{"type":"policy_set","id":"ps-1","attributes":{
		"name":"guardrails","enforcementLevel":"SOFT_MANDATORY","shadowEnforcementLevel":"ADVISORY",
		"global":false,"branch":"main","folder":"/"
	}}}`

	t.Run("create keeps upper case", func(t *testing.T) {
		server, sent := policyTestServer(t, response)
		r := &PolicySetResource{client: newTestClient(server.Client(), server.URL)}
		plan := buildObjectValue(objType, map[string]tftypes.Value{
			"organization_id": str("org-1"), "name": str("guardrails"),
			"enforcement_level": str("SOFT_MANDATORY"), "shadow_enforcement_level": str("advisory"),
			"global": tftypes.NewValue(tftypes.Bool, false), "branch": str("main"), "folder": str("/"),
		})
		resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: plan}}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if got := sentAttributes(sent(http.MethodPost))["shadowEnforcementLevel"]; got != "ADVISORY" {
			t.Errorf("sent shadowEnforcementLevel = %v, want ADVISORY", got)
		}
		if got := policyStateString(t, resp.State, "enforcement_level"); got != "SOFT_MANDATORY" {
			t.Errorf("enforcement_level = %q, want SOFT_MANDATORY as planned", got)
		}
		if got := policyStateString(t, resp.State, "shadow_enforcement_level"); got != "advisory" {
			t.Errorf("shadow_enforcement_level = %q, want advisory as planned", got)
		}
	})

	t.Run("read lower-cases a changed level", func(t *testing.T) {
		server, _ := policyTestServer(t, response)
		r := &PolicySetResource{client: newTestClient(server.Client(), server.URL)}
		prior := buildObjectValue(objType, map[string]tftypes.Value{
			"id": str("ps-1"), "organization_id": str("org-1"),
			"enforcement_level": str("HARD_MANDATORY"), "shadow_enforcement_level": str("ADVISORY"),
		})
		resp := &resource.ReadResponse{State: tfsdk.State{Schema: s, Raw: prior}}
		r.Read(ctx, resource.ReadRequest{State: tfsdk.State{Schema: s, Raw: prior}}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if got := policyStateString(t, resp.State, "enforcement_level"); got != "soft_mandatory" {
			t.Errorf("enforcement_level = %q, want soft_mandatory", got)
		}
		if got := policyStateString(t, resp.State, "shadow_enforcement_level"); got != "ADVISORY" {
			t.Errorf("shadow_enforcement_level = %q, want ADVISORY as in state", got)
		}
	})
}

func TestPolicySetResource_EmptyOpaVersion(t *testing.T) {
	ctx := context.Background()
	s, objType := policySetResourceSchemaAndType(t, ctx)
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	// Terrakube has no OPA version.
	const response = `{"data":{"type":"policy_set","id":"ps-1","attributes":{
		"name":"guardrails","enforcementLevel":"HARD_MANDATORY","global":false,"branch":"main","folder":"/"
	}}}`
	model := map[string]tftypes.Value{
		"organization_id": str("org-1"), "name": str("guardrails"), "enforcement_level": str("hard_mandatory"),
		"global": tftypes.NewValue(tftypes.Bool, false), "branch": str("main"), "folder": str("/"),
		"opa_version": str(""),
	}

	t.Run("create", func(t *testing.T) {
		server, sent := policyTestServer(t, response)
		r := &PolicySetResource{client: newTestClient(server.Client(), server.URL)}
		resp := &resource.CreateResponse{State: tfsdk.State{Schema: s}}
		r.Create(ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: s, Raw: buildObjectValue(objType, model)}}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
		}
		if attributes := sentAttributes(sent(http.MethodPost)); attributes["opaVersion"] != nil {
			t.Errorf("POST attributes = %v, want no opaVersion", attributes)
		}
		if got := policyStateString(t, resp.State, "opa_version"); got != "" {
			t.Errorf("opa_version = %q, want \"\"", got)
		}
	})

	t.Run("update", func(t *testing.T) {
		server, sent := policyTestServer(t, response)
		prior := map[string]tftypes.Value{"id": str("ps-1"), "opa_version": str("0.68.0")}
		plan := map[string]tftypes.Value{"id": str("ps-1"), "name": str("changed")}
		for k, v := range model {
			if _, ok := prior[k]; !ok {
				prior[k] = v
			}
			if _, ok := plan[k]; !ok {
				plan[k] = v
			}
		}
		state := runUpdate(t, &PolicySetResource{client: newTestClient(server.Client(), server.URL)},
			buildObjectValue(objType, prior), buildObjectValue(objType, plan))
		if _, ok := sentAttributes(sent(http.MethodPatch))["opaVersion"]; ok {
			t.Errorf("PATCH attributes = %v, want no opaVersion", sentAttributes(sent(http.MethodPatch)))
		}
		if got := policyStateString(t, state, "opa_version"); got != "" {
			t.Errorf("opa_version = %q, want \"\"", got)
		}
	})

	// Terrakube keeps its values for attributes left out of the PATCH, but the state must keep the
	// planned "" or Terraform reports an inconsistent result.
	t.Run("update keeps a planned empty string while Terrakube keeps its value", func(t *testing.T) {
		server, sent := policyTestServer(t, `{"data":{"type":"policy_set","id":"ps-1","attributes":{
			"name":"changed","enforcementLevel":"HARD_MANDATORY","global":false,"branch":"main","folder":"/policies","opaVersion":"0.68.0"
		}}}`)
		prior := map[string]tftypes.Value{"id": str("ps-1"), "branch": str("main"), "folder": str("/policies"), "opa_version": str("0.68.0")}
		plan := map[string]tftypes.Value{"id": str("ps-1"), "name": str("changed"), "branch": str(""), "folder": str(""), "opa_version": str("")}
		for k, v := range model {
			if _, ok := prior[k]; !ok {
				prior[k] = v
			}
			if _, ok := plan[k]; !ok {
				plan[k] = v
			}
		}
		state := runUpdate(t, &PolicySetResource{client: newTestClient(server.Client(), server.URL)},
			buildObjectValue(objType, prior), buildObjectValue(objType, plan))
		patched := sentAttributes(sent(http.MethodPatch))
		for _, name := range []string{"branch", "folder", "opaVersion"} {
			if _, ok := patched[name]; ok {
				t.Errorf("PATCH attributes = %v, want no %s", patched, name)
			}
		}
		for _, name := range []string{"branch", "folder", "opa_version"} {
			if got := policyStateString(t, state, name); got != "" {
				t.Errorf("%s = %q, want the planned \"\"", name, got)
			}
		}
	})
}
