package provider

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// Terraform prints diagnostic details in full, so an HTML error page or stack
// trace from a failing backend would otherwise become the entire error output.
const maxErrorDetailBytes = 4096

// reportAPIErrorStatus adds a diagnostic naming the HTTP status and the API's own
// error message when the response is not 2xx, and reports whether it did so.
// Call it before unmarshaling a response body: jsonapi rejects an error payload
// with a complaint that mentions neither the status nor the cause.
func reportAPIErrorStatus(diags *diag.Diagnostics, action string, response *http.Response, body []byte) bool {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return false
	}

	diags.AddError(
		fmt.Sprintf("Error %s: %s", action, response.Status),
		apiErrorDetail(body),
	)
	return true
}

func apiErrorDetail(body []byte) string {
	var errorResponse ErrorResponse
	if err := json.Unmarshal(body, &errorResponse); err == nil {
		details := make([]string, 0, len(errorResponse.Errors))
		for _, apiError := range errorResponse.Errors {
			if detail := strings.TrimSpace(html.UnescapeString(apiError.Detail)); detail != "" {
				details = append(details, detail)
			}
		}
		if len(details) > 0 {
			return strings.Join(details, "\n")
		}
	}

	if raw := strings.TrimSpace(string(body)); raw != "" {
		if len(raw) > maxErrorDetailBytes {
			return raw[:maxErrorDetailBytes] + "\n[response body truncated]"
		}
		return raw
	}

	return "The API returned no readable response body."
}
