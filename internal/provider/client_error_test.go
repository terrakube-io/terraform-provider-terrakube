package provider

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	terrakube "github.com/terrakube-io/terrakube-go"
)

func TestReportClientError(t *testing.T) {
	plain := func(n int) string { return strings.Repeat("x", n) }

	tests := []struct {
		name        string
		err         error
		wantSummary string
		wantDetail  string
	}{
		{
			name:        "jsonapi error details are joined",
			err:         &terrakube.APIError{StatusCode: http.StatusConflict, Body: []byte(`{"errors":[{"detail":"duplicate key"},{"detail":"second"}]}`)},
			wantSummary: "Error creating project: 409 Conflict",
			wantDetail:  "duplicate key\nsecond",
		},
		{
			name:        "body shorter than the limit is kept whole",
			err:         &terrakube.APIError{StatusCode: http.StatusInternalServerError, Body: []byte(plain(maxErrorDetailBytes - 1))},
			wantSummary: "Error creating project: 500 Internal Server Error",
			wantDetail:  plain(maxErrorDetailBytes - 1),
		},
		{
			name:        "body at the limit is kept whole",
			err:         &terrakube.APIError{StatusCode: http.StatusInternalServerError, Body: []byte(plain(maxErrorDetailBytes))},
			wantSummary: "Error creating project: 500 Internal Server Error",
			wantDetail:  plain(maxErrorDetailBytes),
		},
		{
			name:        "body beyond the limit is truncated",
			err:         &terrakube.APIError{StatusCode: http.StatusInternalServerError, Body: []byte(plain(maxErrorDetailBytes + 1))},
			wantSummary: "Error creating project: 500 Internal Server Error",
			wantDetail:  plain(maxErrorDetailBytes) + "\n[response body truncated]",
		},
		{
			name:        "wrapped api error is unwrapped",
			err:         errors.Join(errors.New("context"), &terrakube.APIError{StatusCode: http.StatusForbidden}),
			wantSummary: "Error creating project: 403 Forbidden",
			wantDetail:  "The API returned no readable response body.",
		},
		{
			name:        "transport error names the action",
			err:         errors.New("dial tcp: connection refused"),
			wantSummary: "Error creating project",
			wantDetail:  "dial tcp: connection refused",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var diags diag.Diagnostics
			reportClientError(&diags, "creating project", tt.err)

			if diags.ErrorsCount() != 1 {
				t.Fatalf("want one error, got %v", diags)
			}
			if got := diags.Errors()[0].Summary(); got != tt.wantSummary {
				t.Errorf("summary = %q, want %q", got, tt.wantSummary)
			}
			if got := diags.Errors()[0].Detail(); got != tt.wantDetail {
				t.Errorf("detail = %q, want %q", got, tt.wantDetail)
			}
		})
	}
}
