package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// notificationConfigAPI bundles the HTTP client/endpoint/token every notification-related
// resource and data source needs, so the shared helpers below don't need three positional
// arguments each.
type notificationConfigAPI struct {
	client   *http.Client
	endpoint string
	token    string
}

// optionalStringValue converts a nullable API string field into a Terraform types.String,
// distinguishing "the API returned null" (types.StringNull()) from "the API returned an empty
// string" (types.StringValue("")) - a plain types.StringValue(*s) would panic on nil.
func optionalStringValue(s *string) types.String {
	if s == nil {
		return types.StringNull()
	}
	return types.StringValue(*s)
}

// fetchNotificationTriggers reads the full set of trigger resources attached to a notification
// configuration via the related-resource collection endpoint, which - unlike the relationship
// linkage embedded in the configuration's own GET response - returns each trigger's full
// attributes (jobStatus), not just its id. Mirrors workspace_webhook_event_resource.go's Update
// method, which reads its parent webhook's events the same way.
func (a notificationConfigAPI) fetchNotificationTriggers(ctx context.Context, configID string) ([]client.NotificationTriggerEntity, diag.Diagnostics) {
	var diags diag.Diagnostics

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/notification_configuration/%s/triggers", a.endpoint, configID), nil)
	if err != nil {
		diags.AddError("Error creating notification triggers request", fmt.Sprintf("Error creating notification triggers request: %s", err))
		return nil, diags
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := a.client.Do(request)
	if err != nil {
		diags.AddError("Error executing notification triggers request", fmt.Sprintf("Error executing notification triggers request: %s", err))
		return nil, diags
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		diags.AddError("Error reading notification triggers response", fmt.Sprintf("Error reading notification triggers response: %s", err))
		return nil, diags
	}

	if response.StatusCode != http.StatusOK {
		diags.AddError("Error reading notification triggers", fmt.Sprintf("Received non-200 status code: %d, body: %s", response.StatusCode, string(body)))
		return nil, diags
	}

	raw, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.NotificationTriggerEntity)))
	if err != nil {
		diags.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return nil, diags
	}

	triggers := make([]client.NotificationTriggerEntity, 0, len(raw))
	for _, item := range raw {
		triggers = append(triggers, *item.(*client.NotificationTriggerEntity))
	}
	return triggers, diags
}

// syncNotificationTriggers reconciles a configuration's trigger set with the desired list of job
// statuses: POSTs one new trigger per status in desired-but-not-current, and DELETEs one per
// status in current-but-not-desired. Same diff shape as the notification system's own UI
// (EditNotificationConfiguration.tsx's saveTriggers).
func (a notificationConfigAPI) syncNotificationTriggers(ctx context.Context, configID string, current []client.NotificationTriggerEntity, desired []string) diag.Diagnostics {
	var diags diag.Diagnostics

	currentByStatus := make(map[string]string, len(current))
	for _, t := range current {
		currentByStatus[t.JobStatus] = t.ID
	}
	desiredSet := make(map[string]bool, len(desired))
	for _, s := range desired {
		desiredSet[s] = true
	}

	for _, status := range desired {
		if _, ok := currentByStatus[status]; ok {
			continue
		}
		bodyRequest := &client.NotificationTriggerEntity{JobStatus: status}
		out := new(bytes.Buffer)
		if err := jsonapi.MarshalPayload(out, bodyRequest); err != nil {
			diags.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
			return diags
		}
		request, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/notification_configuration/%s/triggers", a.endpoint, configID), strings.NewReader(out.String()))
		if err != nil {
			diags.AddError("Error creating notification trigger request", fmt.Sprintf("Error creating notification trigger request: %s", err))
			return diags
		}
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.token))
		request.Header.Add("Content-Type", "application/vnd.api+json")

		response, err := a.client.Do(request)
		if err != nil {
			diags.AddError("Error executing notification trigger request", fmt.Sprintf("Error executing notification trigger request: %s", err))
			return diags
		}
		body, _ := io.ReadAll(response.Body)
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			diags.AddError(fmt.Sprintf("Failed to create notification trigger %q: %s", status, response.Status), string(body))
			return diags
		}
	}

	for status, triggerID := range currentByStatus {
		if desiredSet[status] {
			continue
		}
		request, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/notification_configuration/%s/triggers/%s", a.endpoint, configID, triggerID), nil)
		if err != nil {
			diags.AddError("Error creating notification trigger delete request", fmt.Sprintf("Error creating notification trigger delete request: %s", err))
			return diags
		}
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.token))

		response, err := a.client.Do(request)
		if err != nil {
			diags.AddError("Error executing notification trigger delete request", fmt.Sprintf("Error executing notification trigger delete request: %s", err))
			return diags
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			body, _ := io.ReadAll(response.Body)
			diags.AddError(fmt.Sprintf("Failed to delete notification trigger %q: %s", status, response.Status), string(body))
			return diags
		}
	}

	return diags
}

// fetchNotificationConfigurationTemplateIDs reads the ids of every template a notification
// configuration is currently narrowed to, via the related-resource collection endpoint (same
// "full resources, not just linkage" shape fetchNotificationTriggers relies on). An empty result
// means "applies to every template" - there's no separate flag for that, it's just the absence of
// any narrowing rows in notification_configuration_template.
func (a notificationConfigAPI) fetchNotificationConfigurationTemplateIDs(ctx context.Context, configID string) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/api/v1/notification_configuration/%s/templates", a.endpoint, configID), nil)
	if err != nil {
		diags.AddError("Error creating notification configuration templates request", fmt.Sprintf("Error creating notification configuration templates request: %s", err))
		return nil, diags
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := a.client.Do(request)
	if err != nil {
		diags.AddError("Error executing notification configuration templates request", fmt.Sprintf("Error executing notification configuration templates request: %s", err))
		return nil, diags
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		diags.AddError("Error reading notification configuration templates response", fmt.Sprintf("Error reading notification configuration templates response: %s", err))
		return nil, diags
	}

	if response.StatusCode != http.StatusOK {
		diags.AddError("Error reading notification configuration templates", fmt.Sprintf("Received non-200 status code: %d, body: %s", response.StatusCode, string(body)))
		return nil, diags
	}

	raw, err := jsonapi.UnmarshalManyPayload(strings.NewReader(string(body)), reflect.TypeOf(new(client.OrganizationTemplateEntity)))
	if err != nil {
		diags.AddError("Error unmarshal payload response", fmt.Sprintf("Error unmarshal payload response: %s", err))
		return nil, diags
	}

	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		ids = append(ids, item.(*client.OrganizationTemplateEntity).ID)
	}
	return ids, diags
}

// replaceNotificationConfigurationTemplates replaces the entire "applies to these templates" set
// in one call, mirroring EditNotificationConfiguration.tsx's saveTemplates: simpler and safer
// than diffing against what was previously selected, and Elide supports replacing a to-many
// relationship wholesale via PATCH .../relationships/{name}. Passing an empty slice clears the
// set (meaning "applies to every template"), it does not skip the call.
func (a notificationConfigAPI) replaceNotificationConfigurationTemplates(ctx context.Context, configID string, templateIDs []string) diag.Diagnostics {
	var diags diag.Diagnostics

	data := make([]map[string]string, 0, len(templateIDs))
	for _, id := range templateIDs {
		data = append(data, map[string]string{"type": "template", "id": id})
	}
	payload := map[string]interface{}{"data": data}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		diags.AddError("Unable to marshal payload", fmt.Sprintf("Unable to marshal payload: %s", err))
		return diags
	}

	request, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("%s/api/v1/notification_configuration/%s/relationships/templates", a.endpoint, configID), bytes.NewReader(jsonData))
	if err != nil {
		diags.AddError("Error creating notification configuration templates request", fmt.Sprintf("Error creating notification configuration templates request: %s", err))
		return diags
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", a.token))
	request.Header.Add("Content-Type", "application/vnd.api+json")

	response, err := a.client.Do(request)
	if err != nil {
		diags.AddError("Error executing notification configuration templates request", fmt.Sprintf("Error executing notification configuration templates request: %s", err))
		return diags
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		diags.AddError(fmt.Sprintf("Failed to set notification configuration templates: %s", response.Status), string(body))
		return diags
	}

	return diags
}
