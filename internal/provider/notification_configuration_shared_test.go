package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"terraform-provider-terrakube/internal/client"
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

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	current := []client.NotificationTriggerEntity{
		{ID: "trig-old", JobStatus: "completed"},
	}
	diags := api.syncNotificationTriggers(ctx, "cfg-1", current, []string{"failed"})
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

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	current := []client.NotificationTriggerEntity{{ID: "trig-1", JobStatus: "failed"}}
	diags := api.syncNotificationTriggers(ctx, "cfg-1", current, []string{"failed"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}

func TestFetchNotificationConfigurationTemplateIDs_ReturnsIDsOnly(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/templates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method %s", r.Method)
		}
		fmt.Fprint(w, `{"data":[
			{"type":"template","id":"tmpl-1","attributes":{"name":"Plan/Apply"}},
			{"type":"template","id":"tmpl-2","attributes":{"name":"Plan/Destroy"}}
		]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	ids, diags := api.fetchNotificationConfigurationTemplateIDs(ctx, "cfg-1")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	if len(ids) != 2 || ids[0] != "tmpl-1" || ids[1] != "tmpl-2" {
		t.Errorf("ids = %v, want [tmpl-1 tmpl-2]", ids)
	}
}

func TestFetchNotificationConfigurationTemplateIDs_EmptyMeansAllTemplates(t *testing.T) {
	ctx := context.Background()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/templates", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[]}`)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	ids, diags := api.fetchNotificationConfigurationTemplateIDs(ctx, "cfg-1")
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty (meaning \"applies to every template\")", ids)
	}
}

func TestReplaceNotificationConfigurationTemplates_SendsFullReplacePatch(t *testing.T) {
	ctx := context.Background()
	var patchedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/relationships/templates", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("unexpected method %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		patchedBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	diags := api.replaceNotificationConfigurationTemplates(ctx, "cfg-1", []string{"tmpl-1", "tmpl-2"})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}

	for _, wantSubstr := range []string{`"type":"template"`, `"id":"tmpl-1"`, `"id":"tmpl-2"`} {
		if !contains(patchedBody, wantSubstr) {
			t.Errorf("expected PATCH body to contain %s, got: %s", wantSubstr, patchedBody)
		}
	}
}

func TestReplaceNotificationConfigurationTemplates_EmptyClearsTheSet(t *testing.T) {
	ctx := context.Background()
	var patchedBody string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/notification_configuration/cfg-1/relationships/templates", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request body: %v", err)
		}
		patchedBody = string(body)
		w.WriteHeader(http.StatusNoContent)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	api := notificationConfigAPI{client: server.Client(), endpoint: server.URL, token: "test-token"}

	diags := api.replaceNotificationConfigurationTemplates(ctx, "cfg-1", []string{})
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
	if wantSubstr := `"data":[]`; !contains(patchedBody, wantSubstr) {
		t.Errorf("expected PATCH body to contain %s (clearing the set), got: %s", wantSubstr, patchedBody)
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
