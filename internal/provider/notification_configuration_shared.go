package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	terrakube "github.com/terrakube-io/terrakube-go"
)

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
// narrowed to, in the order of the "templates" relationship linkage of its GET or list response.
// An empty result means "applies to every template".
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
// plan). nil when neither is set, so the attribute is left out of the request.
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
