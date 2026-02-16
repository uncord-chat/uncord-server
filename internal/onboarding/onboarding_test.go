package onboarding

import (
	"testing"

	"github.com/google/uuid"
	"github.com/uncord-chat/uncord-protocol/models"
)

func TestConfigToModel(t *testing.T) {
	welcomeID := uuid.New()
	role1 := uuid.New()
	role2 := uuid.New()

	cfg := &Config{
		WelcomeChannelID:         &welcomeID,
		RequireEmailVerification: true,
		OpenJoin:                 false,
		MinAccountAgeSeconds:     86400,
		AutoRoles:                []uuid.UUID{role1, role2},
	}

	docs := []models.OnboardingDocument{
		{Slug: "rules", Title: "Rules", Content: "<p>Be nice.</p>", Position: 0, Required: true},
	}

	result := cfg.ToModel(docs)

	if result.WelcomeChannelID == nil || *result.WelcomeChannelID != welcomeID.String() {
		t.Errorf("WelcomeChannelID = %v, want %q", result.WelcomeChannelID, welcomeID.String())
	}
	if !result.RequireEmailVerification {
		t.Error("RequireEmailVerification = false, want true")
	}
	if result.OpenJoin {
		t.Error("OpenJoin = true, want false")
	}
	if result.MinAccountAgeSeconds != 86400 {
		t.Errorf("MinAccountAgeSeconds = %d, want 86400", result.MinAccountAgeSeconds)
	}
	if len(result.AutoRoles) != 2 {
		t.Fatalf("len(AutoRoles) = %d, want 2", len(result.AutoRoles))
	}
	if result.AutoRoles[0] != role1.String() {
		t.Errorf("AutoRoles[0] = %q, want %q", result.AutoRoles[0], role1.String())
	}
	if len(result.Documents) != 1 {
		t.Fatalf("len(Documents) = %d, want 1", len(result.Documents))
	}
	if result.Documents[0].Slug != "rules" {
		t.Errorf("Documents[0].Slug = %q, want %q", result.Documents[0].Slug, "rules")
	}
}

func TestConfigToModelNilWelcomeChannel(t *testing.T) {
	cfg := &Config{
		WelcomeChannelID: nil,
		AutoRoles:        nil,
	}

	result := cfg.ToModel(nil)

	if result.WelcomeChannelID != nil {
		t.Errorf("WelcomeChannelID = %v, want nil", result.WelcomeChannelID)
	}
	if len(result.AutoRoles) != 0 {
		t.Errorf("len(AutoRoles) = %d, want 0", len(result.AutoRoles))
	}
}
