package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"terraform-provider-terrakube/internal/client"

	"github.com/google/jsonapi"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

// notificationConfigAPI bundles the HTTP client/endpoint/token of the data source's net/http
// calls, so the helpers below don't need three positional arguments each. Only
// notification_configuration_data_source.go (and the tests of these helpers) uses it: the
// notification configuration resources are on terrakube-go.
type notificationConfigAPI struct {
	client   *http.Client
	endpoint string
	token    string
}

// notificationTemplatesRelationship is the "templates" relationship of a notification
// configuration create or update. It always replaces the whole set, mirroring
// EditNotificationConfiguration.tsx's saveTemplates: simpler and safer than diffing against what
// was previously selected. An empty list clears the set (meaning "applies to every template"),
// it is not left out.
func notificationTemplatesRelationship(templateIDs []string) map[string]terrakube.Relationship {
	return map[string]terrakube.Relationship{
		"templates": {Type: "template", Many: true, IDs: templateIDs},
	}
}

// notificationTemplateIDs returns the ids of the templates a notification configuration is
// narrowed to, in the order of the "templates" relationship linkage of its GET response. An
// empty result means "applies to every template".
func notificationTemplateIDs(configuration *terrakube.NotificationConfiguration) []string {
	ids := make([]string, 0, len(configuration.Templates))
	for _, template := range configuration.Templates {
		ids = append(ids, template.ID)
	}
	return ids
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

// fetchNotificationTriggers is used only by the data source; the resources read triggers with
// terrakube-go's NotificationTriggers.ListByConfiguration. It reads the full set of trigger resources attached to a notification
// configuration via the related-resource collection endpoint, which - unlike the relationship
// linkage embedded in the configuration's own GET response - returns each trigger's full
// attributes (jobStatus), not just its id. Mirrors workspace_webhook_event_resource.go's Update
// method, which reads its parent webhook's events the same way.
func (a notificationConfigAPI) fetchNotificationTriggers(ctx context.Context, configID string) ([]client.NotificationTriggerEntity, diag.Diagnostics) {
	var diags diag.Diagnostics

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/notification_configuration/%s/triggers", a.endpoint, configID), nil)
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
		trigger, ok := item.(*client.NotificationTriggerEntity)
		if !ok {
			diags.AddError("Error unmarshal payload response", "Unexpected type in notification triggers payload")
			return nil, diags
		}
		triggers = append(triggers, *trigger)
	}
	return triggers, diags
}

// syncNotificationTriggers reconciles a configuration's trigger set with the desired list of job
// statuses: POSTs one new trigger per status in desired-but-not-current, and DELETEs one per
// status in current-but-not-desired, through the top-level /notification_configuration/{id}/triggers
// path. Same diff shape as the notification system's own UI (EditNotificationConfiguration.tsx's
// saveTriggers).
func syncNotificationTriggers(ctx context.Context, c *terrakube.Client, configID string, current []*terrakube.NotificationTrigger, desired []string) diag.Diagnostics {
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
		p := terrakube.Patch{Attributes: map[string]any{"jobStatus": status}}
		if _, err := c.NotificationTriggers.CreateFieldsByConfiguration(ctx, configID, p); err != nil {
			reportClientError(&diags, fmt.Sprintf("creating notification trigger %q", status), err)
			return diags
		}
	}

	for status, triggerID := range currentByStatus {
		if desiredSet[status] {
			continue
		}
		if err := c.NotificationTriggers.DeleteByConfiguration(ctx, configID, triggerID); err != nil {
			reportClientError(&diags, fmt.Sprintf("deleting notification trigger %q", status), err)
			return diags
		}
	}

	return diags
}

// fetchNotificationConfigurationTemplateIDs is used only by the data source.
//
// fetchNotificationConfigurationTemplateIDs reads the ids of every template a notification
// configuration is currently narrowed to, via the related-resource collection endpoint (same
// "full resources, not just linkage" shape fetchNotificationTriggers relies on). An empty result
// means "applies to every template" - there's no separate flag for that, it's just the absence of
// any narrowing rows in notification_configuration_template.
func (a notificationConfigAPI) fetchNotificationConfigurationTemplateIDs(ctx context.Context, configID string) ([]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/notification_configuration/%s/templates", a.endpoint, configID), nil)
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
		template, ok := item.(*client.OrganizationTemplateEntity)
		if !ok {
			diags.AddError("Error unmarshal payload response", "Unexpected type in notification configuration templates payload")
			return nil, diags
		}
		ids = append(ids, template.ID)
	}
	return ids, diags
}

// signingSecretDescription is shared by the organization and workspace notification
// configuration resources.
const signingSecretDescription = "If set, outgoing WEBHOOK requests are signed with an X-Terrakube-Signature header (HMAC-SHA256) so the destination can verify they came from Terrakube. Stored in the Terraform state; conflicts with `signing_secret_wo`. Terrakube never returns the secret, so a change made outside Terraform is not detected, an import leaves it null (the next plan shows an update). Removing it from the configuration clears it in Terrakube."

// signingSecretConfigValidators allows at most one of signing_secret and
// signing_secret_wo: both are optional, a configuration may have no secret.
func signingSecretConfigValidators() []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(
			path.MatchRoot("signing_secret"),
			path.MatchRoot("signing_secret_wo"),
		),
	}
}

// signingSecretToSend returns the signing secret for the request body: signing_secret from
// the plan, or signing_secret_wo from the configuration (write-only values are never in the
// plan). nil when neither is set, so the attribute is left out of the request, as before.
func signingSecretToSend(ctx context.Context, config tfsdk.Config, planned types.String) (*string, diag.Diagnostics) {
	value, fromWo, diags := writeOnlyValue(ctx, config, planned, "signing_secret")
	if diags.HasError() || (planned.IsNull() && !fromWo) {
		return nil, diags
	}
	return &value, diags
}

// signingSecretForUpdate is signingSecretToSend for an update. When the plan has no secret but
// the state had one (signing_secret in the state, or a signing_secret_wo_version, since the
// write-only value itself is never stored), it returns a pointer to an empty string. Terrakube
// signs only when the stored secret is not blank, and a PATCH that leaves the field out keeps
// the old one, so this is how a removal reaches the server.
func signingSecretForUpdate(ctx context.Context, config tfsdk.Config, planned, stateSecret types.String, stateWoVersion types.Int64) (*string, diag.Diagnostics) {
	secret, diags := signingSecretToSend(ctx, config, planned)
	if diags.HasError() || secret != nil {
		return secret, diags
	}
	if !stateSecret.IsNull() || !stateWoVersion.IsNull() {
		empty := ""
		return &empty, diags
	}
	return nil, diags
}
