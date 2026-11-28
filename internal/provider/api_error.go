package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// Terraform prints diagnostic details in full, so an HTML error page or stack
// trace from a failing backend would otherwise become the entire error output.
const maxErrorDetailBytes = 4096

// apiErrorResponse is a JSON:API error document.
type apiErrorResponse struct {
	Errors []struct {
		Detail string `json:"detail"`
	} `json:"errors"`
}

// reportClientError adds a diagnostic for an error returned by the terrakube-go client.
// An API error is reported with the HTTP status and the API's own message; any other error
// (transport, validation) is reported as it is.
func reportClientError(diags *diag.Diagnostics, action string, err error) {
	var apiErr *terrakube.APIError
	if errors.As(err, &apiErr) {
		diags.AddError(
			fmt.Sprintf("Error %s: %d %s", action, apiErr.StatusCode, http.StatusText(apiErr.StatusCode)),
			apiErrorDetail(apiErr.Body),
		)
		return
	}

	diags.AddError(fmt.Sprintf("Error %s", action), err.Error())
}

// reportDeleteError reports an error from a delete and reports whether it did. A 404 is not an
// error: the object is already gone. Any other error, such as a 409 for a registry provider
// version that still has implementations, is reported with reportClientError.
func reportDeleteError(diags *diag.Diagnostics, action string, err error) bool {
	if err == nil || terrakube.IsNotFound(err) {
		return false
	}
	reportClientError(diags, action, err)
	return true
}

func apiErrorDetail(body []byte) string {
	var errorResponse apiErrorResponse
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

// patchFailed reports whether an error from a terrakube-go UpdateFields call means the PATCH
// itself failed. When Terrakube answers a PATCH with 204, UpdateFields reads the object back with
// a GET; an API error from that GET does not undo the update, so it is not a failure here. A
// transport error carries no method and still counts as a failure.
func patchFailed(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *terrakube.APIError
	return !errors.As(err, &apiErr) || apiErr.Method != http.MethodGet
}

// readAfterPatch returns the object a terrakube-go UpdateFields call returned, so an update costs
// one PATCH and at most one GET. UpdateFields returns nil only when its read-back after a 204
// failed, which patchFailed ignores; get then reads the object again. When the read-back answers
// 404, UpdateFields returns an object that holds only the attributes that were sent, so Terraform
// reports an inconsistent result for an object that was deleted right after the PATCH.
func readAfterPatch[T any](updated *T, get func() (*T, error)) (*T, error) {
	if updated != nil {
		return updated, nil
	}
	return get()
}
