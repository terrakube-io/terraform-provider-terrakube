package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// teamTokenHarness talks to a mock of the access-token API, which is plain JSON rather than
// JSON:API: a POST returns the token, a GET lists all tokens and a DELETE answers 202.
type teamTokenHarness struct {
	t       *testing.T
	ctx     context.Context
	res     *TeamTokenResource
	schema  resource.SchemaResponse
	objType tftypes.Object

	token      string
	listStatus int
	listBody   string
	postStatus int
	postBody   string
	delStatus  int

	lastPost   map[string]any
	lastPath   map[string]string // method -> path
	lastAuth   string
	deleteSeen bool
}

func newTeamTokenHarness(t *testing.T) *teamTokenHarness {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"jti": "token-1"}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	h := &teamTokenHarness{
		t: t, ctx: context.Background(), token: signed,
		listStatus: http.StatusOK, postStatus: http.StatusCreated, delStatus: http.StatusAccepted,
		lastPath: map[string]string{},
	}
	h.postBody = fmt.Sprintf(`{"token":%q}`, signed)
	h.listBody = `[{"id":"token-1","description":"ci","days":1,"hours":2,"minutes":3,"group":"TERRAKUBE_ADMIN"}]`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.lastPath[r.Method] = r.URL.Path
		h.lastAuth = r.Header.Get("Authorization")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/access-token/v1/teams":
			raw, _ := io.ReadAll(r.Body)
			h.lastPost = nil
			_ = json.Unmarshal(raw, &h.lastPost)
			w.WriteHeader(h.postStatus)
			fmt.Fprint(w, h.postBody)
		case r.Method == http.MethodGet && r.URL.Path == "/access-token/v1/teams":
			w.WriteHeader(h.listStatus)
			fmt.Fprint(w, h.listBody)
		case r.Method == http.MethodDelete && r.URL.Path == "/access-token/v1/teams/token-1":
			h.deleteSeen = true
			w.WriteHeader(h.delStatus)
			if h.delStatus >= 400 {
				fmt.Fprint(w, `{"errors":[{"detail":"the server said no"}]}`)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	t.Cleanup(server.Close)

	h.res = &TeamTokenResource{client: newTestClient(server.Client(), server.URL)}
	h.res.Schema(h.ctx, resource.SchemaRequest{}, &h.schema)
	objType, ok := h.schema.Schema.Type().TerraformType(h.ctx).(tftypes.Object)
	if !ok {
		t.Fatal("expected schema type to be a tftypes.Object")
	}
	h.objType = objType
	return h
}

func (h *teamTokenHarness) values(id tftypes.Value, extra map[string]tftypes.Value) tftypes.Value {
	str := func(v string) tftypes.Value { return tftypes.NewValue(tftypes.String, v) }
	num := func(v int64) tftypes.Value { return tftypes.NewValue(tftypes.Number, v) }
	values := map[string]tftypes.Value{
		"id": id, "team_name": str("TERRAKUBE_ADMIN"), "description": str("ci"),
		"days": num(1), "hours": num(2), "minutes": num(3),
	}
	for k, v := range extra {
		values[k] = v
	}
	return buildObjectValue(h.objType, values)
}

func (h *teamTokenHarness) state(raw tftypes.Value) tfsdk.State {
	return tfsdk.State{Schema: h.schema.Schema, Raw: raw}
}

func (h *teamTokenHarness) create() *resource.CreateResponse {
	plan := h.values(tftypes.NewValue(tftypes.String, tftypes.UnknownValue),
		map[string]tftypes.Value{"value": tftypes.NewValue(tftypes.String, tftypes.UnknownValue)})
	resp := &resource.CreateResponse{State: h.state(tftypes.NewValue(h.objType, nil))}
	h.res.Create(h.ctx, resource.CreateRequest{Plan: tfsdk.Plan{Schema: h.schema.Schema, Raw: plan}}, resp)
	return resp
}

func (h *teamTokenHarness) created() tfsdk.State {
	h.t.Helper()
	resp := h.create()
	if resp.Diagnostics.HasError() {
		h.t.Fatalf("Create returned diagnostics: %v", resp.Diagnostics)
	}
	return resp.State
}

func (h *teamTokenHarness) read(state tfsdk.State) *resource.ReadResponse {
	resp := &resource.ReadResponse{State: state}
	h.res.Read(h.ctx, resource.ReadRequest{State: state}, resp)
	return resp
}

func (h *teamTokenHarness) attr(state tfsdk.State, name string) string {
	h.t.Helper()
	var values map[string]tftypes.Value
	if err := state.Raw.As(&values); err != nil {
		h.t.Fatal(err)
	}
	var text string
	if err := values[name].As(&text); err == nil {
		return text
	}
	return values[name].String()
}

func TestTeamTokenResource_Create(t *testing.T) {
	h := newTeamTokenHarness(t)
	state := h.created()

	if h.lastPath[http.MethodPost] != "/access-token/v1/teams" || h.lastAuth != "Bearer test-token" {
		t.Errorf("unexpected request: %v auth=%q", h.lastPath, h.lastAuth)
	}
	want := map[string]any{"description": "ci", "days": float64(1), "hours": float64(2), "minutes": float64(3), "group": "TERRAKUBE_ADMIN"}
	for k, v := range want {
		if h.lastPost[k] != v {
			t.Errorf("request %s = %v, want %v", k, h.lastPost[k], v)
		}
	}
	// The ID comes from the token's jti claim, and the token itself is kept in the state.
	if got := h.attr(state, "id"); got != "token-1" {
		t.Errorf("id = %q, want token-1", got)
	}
	if got := h.attr(state, "value"); got != h.token {
		t.Errorf("value is not the token the API returned")
	}
}

func TestTeamTokenResource_CreateErrors(t *testing.T) {
	t.Run("api error", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		h.postStatus = http.StatusForbidden
		h.postBody = `{"errors":[{"detail":"the server said no"}]}`
		resp := h.create()
		if !resp.Diagnostics.HasError() {
			t.Fatal("expected an error diagnostic")
		}
		if !resp.State.Raw.IsNull() {
			t.Error("no state should be stored when the create fails")
		}
	})
	t.Run("a token with no jti claim", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		noID, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "x"}).SignedString([]byte("secret"))
		h.postBody = fmt.Sprintf(`{"token":%q}`, noID)
		if resp := h.create(); !resp.Diagnostics.HasError() {
			t.Error("expected an error when the token has no id")
		}
	})
	t.Run("a response that is not JSON", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		h.postBody = `not json`
		if resp := h.create(); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})
}

func TestTeamTokenResource_Read(t *testing.T) {
	t.Run("refreshes from the list", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		h.listBody = `[{"id":"other","description":"x","days":9,"hours":9,"minutes":9,"group":"G"},` +
			`{"id":"token-1","description":"renamed","days":4,"hours":5,"minutes":6,"group":"TERRAKUBE_ADMIN"}]`
		resp := h.read(state)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Read returned diagnostics: %v", resp.Diagnostics)
		}
		if got := h.attr(resp.State, "description"); got != "renamed" {
			t.Errorf("description = %q, want renamed", got)
		}
		if got := h.attr(resp.State, "value"); got != h.token {
			t.Error("the token must stay in the state, the list does not return it")
		}
		if h.lastPath[http.MethodGet] != "/access-token/v1/teams" {
			t.Errorf("unexpected read path %q", h.lastPath[http.MethodGet])
		}
	})
	t.Run("a token that is gone leaves the state", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		h.listBody = `[]`
		resp := h.read(state)
		if resp.Diagnostics.HasError() {
			t.Fatalf("a missing token is not an error: %v", resp.Diagnostics)
		}
		if !resp.State.Raw.IsNull() {
			t.Error("the resource should be removed from the state")
		}
	})
	t.Run("api error", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		h.listStatus = http.StatusInternalServerError
		if resp := h.read(state); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})
	t.Run("a response that is not JSON", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		h.listBody = `not json`
		if resp := h.read(state); !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})
}

func TestTeamTokenResource_Delete(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		resp := &resource.DeleteResponse{State: state}
		h.res.Delete(h.ctx, resource.DeleteRequest{State: state}, resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("Delete returned diagnostics: %v", resp.Diagnostics)
		}
		if h.lastPath[http.MethodDelete] != "/access-token/v1/teams/token-1" {
			t.Errorf("unexpected delete path %q", h.lastPath[http.MethodDelete])
		}
	})
	for _, status := range []int{http.StatusNoContent, http.StatusNotFound} {
		t.Run(fmt.Sprintf("status %d is not an error", status), func(t *testing.T) {
			h := newTeamTokenHarness(t)
			state := h.created()
			h.delStatus = status
			resp := &resource.DeleteResponse{State: state}
			h.res.Delete(h.ctx, resource.DeleteRequest{State: state}, resp)
			if resp.Diagnostics.HasError() {
				t.Errorf("Delete returned diagnostics: %v", resp.Diagnostics)
			}
		})
	}
	t.Run("api error", func(t *testing.T) {
		h := newTeamTokenHarness(t)
		state := h.created()
		h.delStatus = http.StatusInternalServerError
		resp := &resource.DeleteResponse{State: state}
		h.res.Delete(h.ctx, resource.DeleteRequest{State: state}, resp)
		if !resp.Diagnostics.HasError() {
			t.Error("expected an error diagnostic")
		}
	})
}

func TestTeamTokenResource_ImportState(t *testing.T) {
	h := newTeamTokenHarness(t)
	resp := &resource.ImportStateResponse{State: h.state(tftypes.NewValue(h.objType, nil))}
	h.res.ImportState(h.ctx, resource.ImportStateRequest{ID: "token-9"}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("ImportState returned diagnostics: %v", resp.Diagnostics)
	}
	if got := h.attr(resp.State, "id"); got != "token-9" {
		t.Errorf("id = %q, want token-9", got)
	}
}
