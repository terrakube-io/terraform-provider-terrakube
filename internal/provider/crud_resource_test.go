package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// crudResourceCase describes a resource that lives under an organization and is created,
// read, updated, deleted and imported as `organization_id,id`.
type crudResourceCase struct {
	itemType   string // JSON:API type, for example "tag"
	collection string // full path, for example /api/v1/organization/org-1/tag
	newSource  func(client *http.Client, endpoint string) resource.Resource

	// scope holds the parent IDs a nested resource has, for example project_id.
	scope map[string]tftypes.Value

	// importID, wantImported and badImportIDs default to "org-9,item-9" for organization_id
	// and id. Nested resources have a longer ID.
	importID     string
	wantImported map[string]string
	badImportIDs []string

	// deleteMissingIsOK is set for resources whose Delete treats a 404 as success.
	deleteMissingIsOK bool

	// noOrganization is set for resources with no organization_id, for example federated
	// credentials.
	noOrganization bool

	// deleteMethod is DELETE unless the resource deletes with another request, for example a
	// PATCH that disables it. wantSentDelete are the attributes that request must carry.
	deleteMethod   string
	wantSentDelete map[string]any

	// create is the planned configuration. wantSent are the attributes that must be in the
	// request body, and wantCreated the state afterwards.
	create        map[string]tftypes.Value
	wantSent      map[string]any
	wantCreated   map[string]string
	drift         map[string]any // attributes Terrakube returns after a change made outside Terraform
	wantDrifted   map[string]string
	update        map[string]tftypes.Value
	wantSentPatch map[string]any
	wantUpdated   map[string]string
}

const (
	crudOrgID  = "org-1"
	crudItemID = "item-1"
)

// crudServer is a small Terrakube: it keeps the attributes it was sent, returns them, applies a
// PATCH and honours a DELETE.
type crudServer struct {
	t          *testing.T
	itemType   string
	collection string

	mu       sync.Mutex
	attrs    map[string]any
	deleted  bool
	requests []crudRequest
	// fail makes the next request with that method fail with the given status.
	fail map[string]int
	// versions and implementations are children of the item, for resources that have to delete
	// them first. Both are empty unless a test fills them in.
	versions        []string
	implementations map[string][]string
}

type crudRequest struct {
	method      string
	path        string
	auth        string
	contentType string
	body        map[string]any
}

func (s *crudServer) lastRequest(method string) (crudRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.requests) - 1; i >= 0; i-- {
		if s.requests[i].method == method {
			return s.requests[i], true
		}
	}
	return crudRequest{}, false
}

func (s *crudServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			s.t.Errorf("request body is not JSON: %v: %s", err, raw)
		}
	}
	s.requests = append(s.requests, crudRequest{
		method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"),
		contentType: r.Header.Get("Content-Type"), body: body,
	})

	if status, ok := s.fail[r.Method]; ok {
		delete(s.fail, r.Method)
		w.WriteHeader(status)
		fmt.Fprint(w, `{"errors":[{"detail":"the server said no"}]}`)
		return
	}

	item := s.collection + "/" + crudItemID
	respond := func(status int) {
		w.Header().Set("Content-Type", "application/vnd.api+json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"type": s.itemType, "id": crudItemID, "attributes": s.attrs,
		}})
	}
	attributes := func() map[string]any {
		data, _ := body["data"].(map[string]any)
		attrs, _ := data["attributes"].(map[string]any)
		return attrs
	}

	switch {
	case r.URL.Path == s.collection && r.Method == http.MethodPost:
		s.attrs = attributes()
		respond(http.StatusCreated)
	case r.URL.Path == item && r.Method == http.MethodGet:
		if s.deleted {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respond(http.StatusOK)
	case r.URL.Path == item && r.Method == http.MethodPatch:
		for k, v := range attributes() {
			s.attrs[k] = v
		}
		respond(http.StatusOK)
	case r.URL.Path == item && r.Method == http.MethodDelete:
		s.deleted = true
		w.WriteHeader(http.StatusNoContent)
	case strings.HasPrefix(r.URL.Path, item+"/version") && s.serveChildren(w, r):
	default:
		s.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusTeapot)
	}
}

// serveChildren answers the version and implementation endpoints under the item.
func (s *crudServer) serveChildren(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, s.collection+"/"+crudItemID+"/version"), "/")
	list := func(ids []string) {
		data := make([]map[string]any, len(ids))
		for i, id := range ids {
			data[i] = map[string]any{"type": "x", "id": id}
		}
		w.Header().Set("Content-Type", "application/vnd.api+json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}
	remove := func(ids []string, id string) []string {
		out := []string{}
		for _, existing := range ids {
			if existing != id {
				out = append(out, existing)
			}
		}
		return out
	}
	switch {
	case len(parts) == 1 && r.Method == http.MethodGet:
		list(s.versions)
	case len(parts) == 2 && r.Method == http.MethodDelete:
		if len(s.implementations[parts[1]]) > 0 {
			w.WriteHeader(http.StatusConflict)
			return true
		}
		s.versions = remove(s.versions, parts[1])
		w.WriteHeader(http.StatusNoContent)
	case len(parts) == 3 && parts[2] == "implementation" && r.Method == http.MethodGet:
		list(s.implementations[parts[1]])
	case len(parts) == 4 && parts[2] == "implementation" && r.Method == http.MethodDelete:
		s.implementations[parts[1]] = remove(s.implementations[parts[1]], parts[3])
		w.WriteHeader(http.StatusNoContent)
	default:
		return false
	}
	return true
}

type crudHarness struct {
	t       *testing.T
	tc      crudResourceCase
	ctx     context.Context
	server  *crudServer
	res     resource.Resource
	schema  resource.SchemaResponse
	objType tftypes.Object
}

func newCrudHarness(t *testing.T, tc crudResourceCase) *crudHarness {
	t.Helper()
	srv := &crudServer{t: t, itemType: tc.itemType, collection: tc.collection, fail: map[string]int{}}
	httpServer := httptest.NewServer(srv)
	t.Cleanup(httpServer.Close)

	h := &crudHarness{t: t, tc: tc, ctx: context.Background(), server: srv}
	h.res = tc.newSource(httpServer.Client(), httpServer.URL)
	h.res.Schema(h.ctx, resource.SchemaRequest{}, &h.schema)
	if h.schema.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building schema: %v", h.schema.Diagnostics)
	}
	objType, ok := h.schema.Schema.Type().TerraformType(h.ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	h.objType = objType
	return h
}

func (h *crudHarness) value(id tftypes.Value, attrs map[string]tftypes.Value) tftypes.Value {
	values := map[string]tftypes.Value{"id": id}
	if !h.tc.noOrganization {
		values["organization_id"] = tftypes.NewValue(tftypes.String, crudOrgID)
	}
	for k, v := range h.tc.scope {
		values[k] = v
	}
	for k, v := range attrs {
		values[k] = v
	}
	return buildObjectValue(h.objType, values)
}

func (h *crudHarness) state(raw tftypes.Value) tfsdk.State {
	return tfsdk.State{Schema: h.schema.Schema, Raw: raw}
}

func (h *crudHarness) known() tftypes.Value { return tftypes.NewValue(tftypes.String, crudItemID) }
func (h *crudHarness) unknown() tftypes.Value {
	return tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
}

func (h *crudHarness) create() *resource.CreateResponse {
	plan := h.value(h.unknown(), h.tc.create)
	req := resource.CreateRequest{
		Plan:   tfsdk.Plan{Schema: h.schema.Schema, Raw: plan},
		Config: tfsdk.Config{Schema: h.schema.Schema, Raw: plan},
	}
	resp := &resource.CreateResponse{State: h.state(tftypes.NewValue(h.objType, nil))}
	h.res.Create(h.ctx, req, resp)
	return resp
}

func (h *crudHarness) createdState() tfsdk.State {
	h.t.Helper()
	resp := h.create()
	if resp.Diagnostics.HasError() {
		h.t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.State
}

func (h *crudHarness) read(state tfsdk.State) *resource.ReadResponse {
	resp := &resource.ReadResponse{State: state}
	h.res.Read(h.ctx, resource.ReadRequest{State: state}, resp)
	return resp
}

func (h *crudHarness) update(prior tfsdk.State) *resource.UpdateResponse {
	merged := map[string]tftypes.Value{}
	for k, v := range h.tc.create {
		merged[k] = v
	}
	for k, v := range h.tc.update {
		merged[k] = v
	}
	plan := h.value(h.known(), merged)
	req := resource.UpdateRequest{
		State:  prior,
		Plan:   tfsdk.Plan{Schema: h.schema.Schema, Raw: plan},
		Config: tfsdk.Config{Schema: h.schema.Schema, Raw: plan},
	}
	resp := &resource.UpdateResponse{State: prior}
	h.res.Update(h.ctx, req, resp)
	return resp
}

func (h *crudHarness) delete(prior tfsdk.State) *resource.DeleteResponse {
	resp := &resource.DeleteResponse{State: prior}
	h.res.Delete(h.ctx, resource.DeleteRequest{State: prior}, resp)
	return resp
}

func (h *crudHarness) importState(id string) *resource.ImportStateResponse {
	importer, ok := h.res.(resource.ResourceWithImportState)
	if !ok {
		h.t.Fatal("resource does not implement ImportState")
	}
	resp := &resource.ImportStateResponse{State: h.state(tftypes.NewValue(h.objType, nil))}
	importer.ImportState(h.ctx, resource.ImportStateRequest{ID: id}, resp)
	return resp
}

// attr returns an attribute of the state as a string. Numbers and booleans are formatted, and a
// null is "<null>".
func (h *crudHarness) attr(state tfsdk.State, name string) string {
	h.t.Helper()
	var values map[string]tftypes.Value
	if err := state.Raw.As(&values); err != nil {
		h.t.Fatalf("reading the state: %v", err)
	}
	value, ok := values[name]
	if !ok {
		h.t.Fatalf("the state has no attribute %s", name)
	}
	if value.IsNull() {
		return "<null>"
	}
	var text string
	if err := value.As(&text); err == nil {
		return text
	}
	var flag bool
	if err := value.As(&flag); err == nil {
		return strconv.FormatBool(flag)
	}
	var number *big.Float
	if err := value.As(&number); err == nil {
		return number.Text('f', -1)
	}
	// A list or a set is shown as its elements separated by commas.
	var elements []tftypes.Value
	if err := value.As(&elements); err == nil {
		parts := make([]string, len(elements))
		for i, element := range elements {
			var text string
			if err := element.As(&text); err != nil {
				h.t.Fatalf("attribute %s has an element that is not a string", name)
			}
			parts[i] = text
		}
		return strings.Join(parts, ",")
	}
	h.t.Fatalf("attribute %s has an unsupported type %s", name, value.Type())
	return ""
}

func (h *crudHarness) checkState(state tfsdk.State, want map[string]string) {
	h.t.Helper()
	for name, expected := range want {
		if got := h.attr(state, name); got != expected {
			h.t.Errorf("state %s = %q, want %q", name, got, expected)
		}
	}
}

func bodyAttributes(t *testing.T, request crudRequest) map[string]any {
	t.Helper()
	data, ok := request.body["data"].(map[string]any)
	if !ok {
		t.Fatalf("request body has no data: %v", request.body)
	}
	attrs, _ := data["attributes"].(map[string]any)
	return attrs
}

func checkSent(t *testing.T, request crudRequest, want map[string]any) {
	t.Helper()
	attrs := bodyAttributes(t, request)
	for name, expected := range want {
		if got := attrs[name]; fmt.Sprint(got) != fmt.Sprint(expected) {
			t.Errorf("request attribute %s = %v, want %v", name, got, expected)
		}
	}
}

func runCrudResourceTests(t *testing.T, tc crudResourceCase) {
	t.Helper()
	deleteMethod := tc.deleteMethod
	if deleteMethod == "" {
		deleteMethod = http.MethodDelete
	}

	t.Run("create", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		state := h.createdState()

		request, ok := h.server.lastRequest(http.MethodPost)
		if !ok || request.path != tc.collection {
			t.Fatalf("expected a POST to %s, got %+v", tc.collection, request)
		}
		if request.auth != "Bearer test-token" || request.contentType != "application/vnd.api+json" {
			t.Errorf("unexpected headers: auth=%q content-type=%q", request.auth, request.contentType)
		}
		data, _ := request.body["data"].(map[string]any)
		if data["type"] != tc.itemType {
			t.Errorf("request type = %v, want %s", data["type"], tc.itemType)
		}
		checkSent(t, request, tc.wantSent)

		want := map[string]string{"id": crudItemID}
		if !tc.noOrganization {
			want["organization_id"] = crudOrgID
		}
		for k, v := range tc.wantCreated {
			want[k] = v
		}
		h.checkState(state, want)
	})

	t.Run("create reports an API error", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		h.server.fail[http.MethodPost] = http.StatusForbidden
		resp := h.create()
		if !resp.Diagnostics.HasError() {
			t.Fatal("expected an error diagnostic")
		}
		if !resp.State.Raw.IsNull() {
			t.Error("no state should be stored when the create fails")
		}
	})

	t.Run("read picks up a change made outside Terraform", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		state := h.createdState()
		for k, v := range tc.drift {
			h.server.attrs[k] = v
		}
		resp := h.read(state)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
		}
		h.checkState(resp.State, tc.wantDrifted)

		request, _ := h.server.lastRequest(http.MethodGet)
		if request.path != tc.collection+"/"+crudItemID || request.auth != "Bearer test-token" {
			t.Errorf("unexpected read request: %+v", request)
		}
	})

	t.Run("read removes a resource that no longer exists", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		state := h.createdState()
		h.server.deleted = true
		resp := h.read(state)
		if resp.Diagnostics.HasError() {
			t.Fatalf("a 404 must not be an error: %v", resp.Diagnostics)
		}
		if !resp.State.Raw.IsNull() {
			t.Error("the resource should be removed from the state on a 404")
		}
	})

	t.Run("read reports an API error", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		state := h.createdState()
		h.server.fail[http.MethodGet] = http.StatusInternalServerError
		if resp := h.read(state); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})

	t.Run("update", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		prior := h.createdState()
		resp := h.update(prior)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Update returned diagnostics: %v", resp.Diagnostics)
		}

		request, ok := h.server.lastRequest(http.MethodPatch)
		if !ok || request.path != tc.collection+"/"+crudItemID {
			t.Fatalf("expected a PATCH to the item, got %+v", request)
		}
		if data, _ := request.body["data"].(map[string]any); data["id"] != crudItemID {
			t.Errorf("the PATCH body must carry the id, got %v", data["id"])
		}
		checkSent(t, request, tc.wantSentPatch)
		h.checkState(resp.State, tc.wantUpdated)

		// Update reads the item again after the PATCH, so the state holds what Terrakube stored and
		// not only what was planned. The mock returns what it was sent, so the request is the proof.
		h.server.mu.Lock()
		defer h.server.mu.Unlock()
		patched := false
		reread := false
		for _, r := range h.server.requests {
			switch {
			case r.method == http.MethodPatch:
				patched = true
			case patched && r.method == http.MethodGet && r.path == tc.collection+"/"+crudItemID:
				reread = true
			}
		}
		if !reread {
			t.Error("expected a GET of the item after the PATCH")
		}
	})

	t.Run("update reports an error when the item cannot be read afterwards", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		prior := h.createdState()
		h.server.fail[http.MethodGet] = http.StatusInternalServerError
		if resp := h.update(prior); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})

	t.Run("update reports an API error", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		prior := h.createdState()
		h.server.fail[http.MethodPatch] = http.StatusConflict
		if resp := h.update(prior); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})

	t.Run("delete", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		prior := h.createdState()
		resp := h.delete(prior)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Delete returned diagnostics: %v", resp.Diagnostics)
		}
		request, ok := h.server.lastRequest(deleteMethod)
		if !ok || request.path != tc.collection+"/"+crudItemID {
			t.Errorf("expected a %s of the item, got %+v", deleteMethod, request)
		}
		if tc.wantSentDelete != nil {
			checkSent(t, request, tc.wantSentDelete)
		}
	})

	t.Run("delete reports an API error", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		prior := h.createdState()
		h.server.fail[deleteMethod] = http.StatusInternalServerError
		if resp := h.delete(prior); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})

	if tc.deleteMissingIsOK {
		t.Run("delete succeeds when the resource is already gone", func(t *testing.T) {
			h := newCrudHarness(t, tc)
			prior := h.createdState()
			h.server.fail[deleteMethod] = http.StatusNotFound
			if resp := h.delete(prior); resp.Diagnostics.HasError() {
				t.Errorf("a 404 on delete must not be an error: %v", resp.Diagnostics)
			}
		})
	}

	t.Run("import", func(t *testing.T) {
		h := newCrudHarness(t, tc)
		importID, want, bad := tc.importID, tc.wantImported, tc.badImportIDs
		if importID == "" {
			importID = "org-9,item-9"
			want = map[string]string{"organization_id": "org-9", "id": "item-9"}
			bad = []string{"item-9", "org-9,", ",item-9", "org-9,item-9,extra", ""}
		}

		resp := h.importState(importID)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ImportState returned diagnostics: %v", resp.Diagnostics)
		}
		h.checkState(resp.State, want)

		for _, id := range bad {
			if resp := h.importState(id); !resp.Diagnostics.HasError() {
				t.Errorf("expected an error for import ID %q", id)
			}
		}
	})
}
