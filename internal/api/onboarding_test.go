package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/models"

	"github.com/uncord-chat/uncord-server/internal/member"
	"github.com/uncord-chat/uncord-server/internal/onboarding"
	"github.com/uncord-chat/uncord-server/internal/user"
)

// stubMemberRepo implements member.Repository for GetOnboardingStatus tests. Only GetStatus and Activate are
// functional; calling any other method panics via the embedded nil interface.
type stubMemberRepo struct {
	member.Repository
	status      string
	err         error
	activateErr error
	activated   bool
}

func (r *stubMemberRepo) GetStatus(context.Context, uuid.UUID) (string, error) {
	return r.status, r.err
}

func (r *stubMemberRepo) Activate(_ context.Context, userID uuid.UUID, _ []uuid.UUID) (*member.WithProfile, error) {
	if r.activateErr != nil {
		return nil, r.activateErr
	}
	r.activated = true
	return &member.WithProfile{UserID: userID, Status: models.MemberStatusActive}, nil
}

// stubOnboardingRepo implements onboarding.Repository for GetOnboardingStatus tests.
type stubOnboardingRepo struct {
	cfg *onboarding.Config
}

func (r *stubOnboardingRepo) Get(context.Context) (*onboarding.Config, error) {
	return r.cfg, nil
}

func (r *stubOnboardingRepo) Update(context.Context, onboarding.UpdateParams) (*onboarding.Config, error) {
	panic("Update not used in status tests")
}

func (r *stubOnboardingRepo) RecordAcceptances(context.Context, uuid.UUID, []string) error {
	return nil
}

func (r *stubOnboardingRepo) GetAcceptances(context.Context, uuid.UUID) ([]onboarding.Acceptance, error) {
	return nil, nil
}

// stubUserRepo implements user.Repository for GetOnboardingStatus tests. Only GetByID is functional; calling any other
// method panics via the embedded nil interface.
type stubUserRepo struct {
	user.Repository
	u *user.User
}

func (r *stubUserRepo) GetByID(context.Context, uuid.UUID) (*user.User, error) {
	return r.u, nil
}

// testDocumentStore creates a DocumentStore from a temp directory containing a single required document. The directory
// is cleaned up automatically when the test finishes.
func testDocumentStore(t *testing.T) *onboarding.DocumentStore {
	t.Helper()
	dir := t.TempDir()
	docsDir := filepath.Join(dir, "documents")
	if err := os.Mkdir(docsDir, 0o755); err != nil {
		t.Fatalf("create documents dir: %v", err)
	}
	manifest := `{"documents":[{"slug":"rules","title":"Rules","file":"rules.md","position":1,"required":true}]}`
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docsDir, "rules.md"), []byte("Follow the rules."), 0o644); err != nil {
		t.Fatalf("write document: %v", err)
	}
	store, err := onboarding.LoadDocuments(dir)
	if err != nil {
		t.Fatalf("load documents: %v", err)
	}
	return store
}

func TestGetOnboardingStatus(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	emptyDocs := onboarding.EmptyDocumentStore()
	requiredDocs := testDocumentStore(t)

	tests := []struct {
		name                     string
		emailVerified            bool
		requireEmailVerification bool
		memberStatus             string
		memberErr                error
		documents                *onboarding.DocumentStore
		wantStep                 models.OnboardingStep
		wantActivated            bool
	}{
		{
			name:                     "unverified email with verification required returns verify_email",
			emailVerified:            false,
			requireEmailVerification: true,
			memberErr:                member.ErrNotFound,
			wantStep:                 models.OnboardingStepVerifyEmail,
		},
		{
			name:                     "verified email not joined returns join_server",
			emailVerified:            true,
			requireEmailVerification: true,
			memberErr:                member.ErrNotFound,
			wantStep:                 models.OnboardingStepJoinServer,
		},
		{
			name:                     "unverified email verification not required returns join_server",
			emailVerified:            false,
			requireEmailVerification: false,
			memberErr:                member.ErrNotFound,
			wantStep:                 models.OnboardingStepJoinServer,
		},
		{
			name:                     "pending member with required documents returns accept_documents",
			emailVerified:            true,
			requireEmailVerification: false,
			memberStatus:             models.MemberStatusPending,
			documents:                requiredDocs,
			wantStep:                 models.OnboardingStepAcceptDocuments,
		},
		{
			name:                     "pending member with no required documents auto-activates and returns complete",
			emailVerified:            true,
			requireEmailVerification: false,
			memberStatus:             models.MemberStatusPending,
			documents:                emptyDocs,
			wantStep:                 models.OnboardingStepComplete,
			wantActivated:            true,
		},
		{
			name:                     "active member returns complete",
			emailVerified:            true,
			requireEmailVerification: false,
			memberStatus:             models.MemberStatusActive,
			wantStep:                 models.OnboardingStepComplete,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			memberRepo := &stubMemberRepo{status: tt.memberStatus, err: tt.memberErr}

			handler := NewOnboardingHandler(
				&stubOnboardingRepo{cfg: &onboarding.Config{RequireEmailVerification: tt.requireEmailVerification}},
				tt.documents,
				memberRepo,
				&stubUserRepo{u: &user.User{ID: userID, EmailVerified: tt.emailVerified}},
				nil, nil, nil,
				zerolog.Nop(),
			)

			app := fiber.New()
			app.Use(fakeAuth(userID))
			app.Get("/status", handler.GetOnboardingStatus)

			req := httptest.NewRequestWithContext(context.Background(),http.MethodGet, "/status", nil)
			resp := doReq(t, app, req)
			body := readBody(t, resp)

			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status = %d, want %d; body = %s", resp.StatusCode, fiber.StatusOK, body)
			}

			env := parseSuccess(t, body)
			var got models.OnboardingStatusResponse
			if err := json.Unmarshal(env.Data, &got); err != nil {
				t.Fatalf("unmarshal status response: %v", err)
			}
			if got.Step != tt.wantStep {
				t.Errorf("step = %q, want %q", got.Step, tt.wantStep)
			}
			if memberRepo.activated != tt.wantActivated {
				t.Errorf("activated = %v, want %v", memberRepo.activated, tt.wantActivated)
			}
		})
	}
}
