package client

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/jsonapi"
)

func TestNotificationConfigurationEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	description := "sample description"
	secret := "sample-secret"
	original := &NotificationConfigurationEntity{
		Name:           "prod-alerts",
		Description:    &description,
		ChannelType:    "SLACK",
		DestinationUrl: "https://hooks.slack.com/services/x",
		SigningSecret:  &secret,
		Active:         true,
		MessageStyle:   "SIMPLE",
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &NotificationConfigurationEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.Name != original.Name {
		t.Errorf("Name = %q, want %q", roundTripped.Name, original.Name)
	}
	if roundTripped.ChannelType != original.ChannelType {
		t.Errorf("ChannelType = %q, want %q", roundTripped.ChannelType, original.ChannelType)
	}
	if roundTripped.DestinationUrl != original.DestinationUrl {
		t.Errorf("DestinationUrl = %q, want %q", roundTripped.DestinationUrl, original.DestinationUrl)
	}
	if roundTripped.Description == nil || *roundTripped.Description != description {
		t.Errorf("Description = %v, want %q", roundTripped.Description, description)
	}
	if roundTripped.SigningSecret == nil || *roundTripped.SigningSecret != secret {
		t.Errorf("SigningSecret = %v, want %q", roundTripped.SigningSecret, secret)
	}
	if !roundTripped.Active {
		t.Errorf("Active = false, want true")
	}
	if roundTripped.MessageStyle != original.MessageStyle {
		t.Errorf("MessageStyle = %q, want %q", roundTripped.MessageStyle, original.MessageStyle)
	}
}

func TestNotificationTriggerEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := &NotificationTriggerEntity{JobStatus: "failed"}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &NotificationTriggerEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.JobStatus != original.JobStatus {
		t.Errorf("JobStatus = %q, want %q", roundTripped.JobStatus, original.JobStatus)
	}
}

func TestPolicySetEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	desc := "Production guardrails"
	shadow := "ADVISORY"
	overrideTeam := "secops-approvers"
	repo := "https://github.com/enterprise/policies.git"

	original := &PolicySetEntity{
		Name:                   "blast-radius",
		Description:            &desc,
		EnforcementLevel:       "HARD_MANDATORY",
		ShadowEnforcementLevel: &shadow,
		OverrideTeam:           &overrideTeam,
		Global:                 true,
		Repository:             &repo,
		Branch:                 "v2.1.0",
		Folder:                 "bundles/blast_radius",
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &PolicySetEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.Name != original.Name {
		t.Errorf("Name = %q, want %q", roundTripped.Name, original.Name)
	}
	if roundTripped.EnforcementLevel != original.EnforcementLevel {
		t.Errorf("EnforcementLevel = %q, want %q", roundTripped.EnforcementLevel, original.EnforcementLevel)
	}
	if !roundTripped.Global {
		t.Errorf("Global = false, want true")
	}
	if roundTripped.Branch != "v2.1.0" {
		t.Errorf("Branch = %q, want v2.1.0", roundTripped.Branch)
	}
	if roundTripped.Folder != "bundles/blast_radius" {
		t.Errorf("Folder = %q, want bundles/blast_radius", roundTripped.Folder)
	}
	if roundTripped.Description == nil || *roundTripped.Description != desc {
		t.Errorf("Description = %v, want %q", roundTripped.Description, desc)
	}
	if roundTripped.ShadowEnforcementLevel == nil || *roundTripped.ShadowEnforcementLevel != shadow {
		t.Errorf("ShadowEnforcementLevel = %v, want %q", roundTripped.ShadowEnforcementLevel, shadow)
	}
	if roundTripped.OverrideTeam == nil || *roundTripped.OverrideTeam != overrideTeam {
		t.Errorf("OverrideTeam = %v, want %q", roundTripped.OverrideTeam, overrideTeam)
	}
	if roundTripped.Repository == nil || *roundTripped.Repository != repo {
		t.Errorf("Repository = %v, want %q", roundTripped.Repository, repo)
	}
}

func TestPolicyAttachmentEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := &PolicyAttachmentEntity{
		ID: "att-123",
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &PolicyAttachmentEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.ID != original.ID {
		t.Errorf("ID = %q, want %q", roundTripped.ID, original.ID)
	}
}

func TestPolicyExemptionEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	expiry := "2026-12-31T23:59:59Z"
	original := &PolicyExemptionEntity{
		RuleId:          "azure_apim_no_public_network",
		TicketReference: "SEC-8842",
		Justification:   "Approved third-party integration gateway",
		ExpiresAt:       &expiry,
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &PolicyExemptionEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.RuleId != original.RuleId {
		t.Errorf("RuleId = %q, want %q", roundTripped.RuleId, original.RuleId)
	}
	if roundTripped.TicketReference != original.TicketReference {
		t.Errorf("TicketReference = %q, want %q", roundTripped.TicketReference, original.TicketReference)
	}
	if roundTripped.Justification != original.Justification {
		t.Errorf("Justification = %q, want %q", roundTripped.Justification, original.Justification)
	}
	if roundTripped.ExpiresAt == nil || *roundTripped.ExpiresAt != expiry {
		t.Errorf("ExpiresAt = %v, want %q", roundTripped.ExpiresAt, expiry)
	}
}

func TestWorkspaceEntity_PolicyComplianceStatus_MarshalUnmarshalRoundTrip(t *testing.T) {
	original := &WorkspaceEntity{
		Name:                   "test-workspace",
		Source:                 "https://github.com/org/repo",
		Branch:                 "main",
		Folder:                 "/",
		IaCType:                "terraform",
		IaCVersion:             "1.15.8",
		ExecutionMode:          "remote",
		PolicyComplianceStatus: "COMPLIANT",
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &WorkspaceEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.PolicyComplianceStatus != "COMPLIANT" {
		t.Errorf("PolicyComplianceStatus = %q, want COMPLIANT", roundTripped.PolicyComplianceStatus)
	}
}

func TestPolicySetParameterEntity_MarshalUnmarshalRoundTrip(t *testing.T) {
	desc := "Maximum allowed resource count"
	original := &PolicySetParameterEntity{
		Key:         "max_resources",
		Value:       "50",
		Description: &desc,
		PolicySet:   &PolicySetEntity{ID: "ps-456"},
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &PolicySetParameterEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.Key != original.Key {
		t.Errorf("Key = %q, want %q", roundTripped.Key, original.Key)
	}
	if roundTripped.Value != original.Value {
		t.Errorf("Value = %q, want %q", roundTripped.Value, original.Value)
	}
	if roundTripped.Description == nil || *roundTripped.Description != desc {
		t.Errorf("Description = %v, want %q", roundTripped.Description, desc)
	}
	if roundTripped.PolicySet == nil || roundTripped.PolicySet.ID != "ps-456" {
		t.Errorf("PolicySet = %v, want ID 'ps-456'", roundTripped.PolicySet)
	}
}

func TestPolicySetEntity_OpaVersion_MarshalUnmarshalRoundTrip(t *testing.T) {
	opaVer := "0.68.0"
	original := &PolicySetEntity{
		Name:             "opa-ver-test",
		EnforcementLevel: "HARD_MANDATORY",
		Global:           false,
		OpaVersion:       &opaVer,
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &PolicySetEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.OpaVersion == nil || *roundTripped.OpaVersion != opaVer {
		t.Errorf("OpaVersion = %v, want %q", roundTripped.OpaVersion, opaVer)
	}
}

func TestTeamEntity_ManagePolicies_MarshalUnmarshalRoundTrip(t *testing.T) {
	managePolicies := true
	original := &TeamEntity{
		Name:           "secops-admins",
		ManageState:    true,
		ManagePolicies: &managePolicies,
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	roundTripped := &TeamEntity{}
	if err := jsonapi.UnmarshalPayload(strings.NewReader(out.String()), roundTripped); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if roundTripped.ManagePolicies == nil || *roundTripped.ManagePolicies != true {
		t.Errorf("ManagePolicies = %v, want true", roundTripped.ManagePolicies)
	}
}

func TestTeamEntity_ManagePolicies_OmitWhenNil(t *testing.T) {
	original := &TeamEntity{
		Name:        "legacy-team",
		ManageState: true,
	}

	var out bytes.Buffer
	if err := jsonapi.MarshalPayload(&out, original); err != nil {
		t.Fatalf("MarshalPayload: %v", err)
	}

	rawJSON := out.String()
	if strings.Contains(rawJSON, "managePolicies") {
		t.Errorf("Expected managePolicies to be omitted from JSON when nil, got: %s", rawJSON)
	}
}
