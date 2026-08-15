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
