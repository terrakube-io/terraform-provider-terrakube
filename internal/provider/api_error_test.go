package provider

import (
	"net/http"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

func TestReportAPIErrorStatus(t *testing.T) {
	tests := []struct {
		name        string
		statusCode  int
		status      string
		body        string
		wantError   bool
		wantSummary string
		wantDetail  string
	}{
		{
			name:       "success passes through",
			statusCode: http.StatusCreated,
			status:     "201 Created",
			body:       `{"data":{"type":"globalvar","id":"1"}}`,
			wantError:  false,
		},
		{
			name:        "jsonapi error detail is surfaced",
			statusCode:  http.StatusConflict,
			status:      "409 Conflict",
			body:        `{"errors":[{"detail":"duplicate key VAR_NAME"}]}`,
			wantError:   true,
			wantSummary: "Error creating organization variable: 409 Conflict",
			wantDetail:  "duplicate key VAR_NAME",
		},
		{
			name:        "html entities in detail are decoded",
			statusCode:  http.StatusBadRequest,
			status:      "400 Bad Request",
			body:        `{"errors":[{"detail":"value &quot;a&quot; &amp; &quot;b&quot; rejected"}]}`,
			wantError:   true,
			wantSummary: "Error creating organization variable: 400 Bad Request",
			wantDetail:  `value "a" & "b" rejected`,
		},
		{
			name:        "non-json body is surfaced verbatim",
			statusCode:  http.StatusInternalServerError,
			status:      "500 Internal Server Error",
			body:        "<html>upstream failure</html>",
			wantError:   true,
			wantSummary: "Error creating organization variable: 500 Internal Server Error",
			wantDetail:  "<html>upstream failure</html>",
		},
		{
			name:        "empty body still names the status",
			statusCode:  http.StatusForbidden,
			status:      "403 Forbidden",
			body:        "",
			wantError:   true,
			wantSummary: "Error creating organization variable: 403 Forbidden",
			wantDetail:  "The API returned no readable response body.",
		},
		{
			name:        "error list without details falls back to the body",
			statusCode:  http.StatusBadRequest,
			status:      "400 Bad Request",
			body:        `{"errors":[]}`,
			wantError:   true,
			wantSummary: "Error creating organization variable: 400 Bad Request",
			wantDetail:  `{"errors":[]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var diags diag.Diagnostics
			response := &http.Response{StatusCode: test.statusCode, Status: test.status}

			reported := reportAPIErrorStatus(&diags, "creating organization variable", response, []byte(test.body))

			if reported != test.wantError {
				t.Fatalf("expected reported=%v, got %v", test.wantError, reported)
			}
			if reported != diags.HasError() {
				t.Fatalf("return value %v disagrees with diagnostics %v", reported, diags)
			}
			if !test.wantError {
				return
			}
			if got := diags[0].Summary(); got != test.wantSummary {
				t.Errorf("summary: expected %q, got %q", test.wantSummary, got)
			}
			if got := diags[0].Detail(); got != test.wantDetail {
				t.Errorf("detail: expected %q, got %q", test.wantDetail, got)
			}
		})
	}
}

func TestApiErrorDetailTruncatesLongBody(t *testing.T) {
	detail := apiErrorDetail([]byte("<html>" + strings.Repeat("x", 2*maxErrorDetailBytes) + "</html>"))

	if len(detail) > maxErrorDetailBytes+len("\n[response body truncated]") {
		t.Fatalf("expected detail to be capped, got %d bytes", len(detail))
	}
	if !strings.HasSuffix(detail, "[response body truncated]") {
		t.Errorf("expected truncation marker, got tail %q", detail[len(detail)-40:])
	}
}

func TestApiErrorDetailJoinsMultipleErrors(t *testing.T) {
	detail := apiErrorDetail([]byte(`{"errors":[{"detail":"first problem"},{"detail":""},{"detail":"second problem"}]}`))

	if !strings.Contains(detail, "first problem") || !strings.Contains(detail, "second problem") {
		t.Fatalf("expected both details, got %q", detail)
	}
	if strings.Count(detail, "\n") != 1 {
		t.Errorf("expected empty detail to be dropped, got %q", detail)
	}
}
