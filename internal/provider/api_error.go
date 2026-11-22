package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	terrakube "github.com/terrakube-io/terrakube-go"
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

// reportClientError adds a diagnostic for an error returned by the terrakube-go client.
// An API error is reported like reportAPIErrorStatus does, naming the HTTP status and the
// API's own message; any other error (transport, validation) is reported as it is.
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

// reportDeleteFailure reads and closes the response to a DELETE, and adds an error diagnostic
// when it failed. A 404 is not a failure: the object is already gone, which is what the delete
// wanted. Without this check, an error such as a 403 or a 500 would count as a successful
// delete and the object would leave the state while it still exists.
func reportDeleteFailure(diags *diag.Diagnostics, action string, response *http.Response) {
	defer response.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode == http.StatusNotFound {
		return
	}
	reportAPIErrorStatus(diags, action, response, body)
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
