package member

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	apierrors "github.com/uncord-chat/uncord-protocol/errors"
	"github.com/uncord-chat/uncord-protocol/models"
)

// fakeStatusRepo implements the subset of Repository exercised by RequireActiveMember.
type fakeStatusRepo struct {
	statuses map[uuid.UUID]string
}

func (f *fakeStatusRepo) GetStatus(_ context.Context, userID uuid.UUID) (string, error) {
	s, ok := f.statuses[userID]
	if !ok {
		return "", ErrNotFound
	}
	return s, nil
}

// Unused interface methods required by Repository.
func (f *fakeStatusRepo) List(context.Context, *uuid.UUID, int) ([]MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) GetByUserID(context.Context, uuid.UUID) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) GetByUserIDAnyStatus(context.Context, uuid.UUID) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) UpdateNickname(context.Context, uuid.UUID, *string) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) Delete(context.Context, uuid.UUID) error { panic("not implemented") }
func (f *fakeStatusRepo) SetTimeout(context.Context, uuid.UUID, time.Time) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) ClearTimeout(context.Context, uuid.UUID) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) Ban(context.Context, uuid.UUID, uuid.UUID, *string, *time.Time) error {
	panic("not implemented")
}
func (f *fakeStatusRepo) Unban(context.Context, uuid.UUID) error { panic("not implemented") }
func (f *fakeStatusRepo) ListBans(context.Context, *uuid.UUID, int) ([]BanRecord, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) IsBanned(context.Context, uuid.UUID) (bool, error) { panic("not implemented") }
func (f *fakeStatusRepo) AssignRole(context.Context, uuid.UUID, uuid.UUID) error {
	panic("not implemented")
}
func (f *fakeStatusRepo) RemoveRole(context.Context, uuid.UUID, uuid.UUID) error {
	panic("not implemented")
}
func (f *fakeStatusRepo) CreatePending(context.Context, uuid.UUID) (*MemberWithProfile, error) {
	panic("not implemented")
}
func (f *fakeStatusRepo) Activate(context.Context, uuid.UUID, []uuid.UUID) (*MemberWithProfile, error) {
	panic("not implemented")
}

func TestRequireActiveMember(t *testing.T) {
	t.Parallel()

	activeID := uuid.New()
	pendingID := uuid.New()
	timedOutID := uuid.New()
	nonMemberID := uuid.New()

	repo := &fakeStatusRepo{
		statuses: map[uuid.UUID]string{
			activeID:   models.MemberStatusActive,
			pendingID:  models.MemberStatusPending,
			timedOutID: models.MemberStatusTimedOut,
		},
	}
	mw := RequireActiveMember(repo)

	tests := []struct {
		name       string
		userID     uuid.UUID
		setLocals  bool
		wantStatus int
		wantCode   string
	}{
		{
			name:       "active member passes through",
			userID:     activeID,
			setLocals:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "timed out member passes through",
			userID:     timedOutID,
			setLocals:  true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "pending member is blocked",
			userID:     pendingID,
			setLocals:  true,
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.MembershipRequired),
		},
		{
			name:       "non member is blocked",
			userID:     nonMemberID,
			setLocals:  true,
			wantStatus: http.StatusForbidden,
			wantCode:   string(apierrors.MembershipRequired),
		},
		{
			name:       "missing locals is blocked",
			setLocals:  false,
			wantStatus: http.StatusUnauthorized,
			wantCode:   string(apierrors.Unauthorised),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			app := fiber.New()

			app.Use(func(c fiber.Ctx) error {
				if tt.setLocals {
					c.Locals("userID", tt.userID)
				}
				return c.Next()
			})
			app.Get("/test", mw, func(c fiber.Ctx) error {
				return c.SendStatus(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("app.Test: %v", err)
			}
			defer func() { _ = resp.Body.Close() }()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantCode != "" {
				bodyBytes, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				var errResp struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(bodyBytes, &errResp); err != nil {
					t.Fatalf("unmarshal error: %v", err)
				}
				if errResp.Error.Code != tt.wantCode {
					t.Errorf("error code = %q, want %q", errResp.Error.Code, tt.wantCode)
				}
			}
		})
	}
}
