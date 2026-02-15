package permission

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/uncord-chat/uncord-protocol/permissions"
)

// --- Fake Store ---

type fakeStore struct {
	isOwner         bool
	isOwnerErr      error
	roleEntries     []RolePermEntry
	roleErr         error
	chanInfo        ChannelInfo
	chanInfoErr     error
	overrides       map[string][]Override // keyed by "type:id"
	overridesErr    error
	isOwnerCalled   bool
	roleCalled      bool
	chanInfoCalled  bool
	overridesCalled int
}

func (s *fakeStore) IsOwner(_ context.Context, _ uuid.UUID) (bool, error) {
	s.isOwnerCalled = true
	return s.isOwner, s.isOwnerErr
}

func (s *fakeStore) RolePermissions(_ context.Context, _ uuid.UUID) ([]RolePermEntry, error) {
	s.roleCalled = true
	return s.roleEntries, s.roleErr
}

func (s *fakeStore) ChannelInfo(_ context.Context, _ uuid.UUID) (ChannelInfo, error) {
	s.chanInfoCalled = true
	return s.chanInfo, s.chanInfoErr
}

func (s *fakeStore) Overrides(_ context.Context, targetType TargetType, targetID uuid.UUID) ([]Override, error) {
	s.overridesCalled++
	if s.overridesErr != nil {
		return nil, s.overridesErr
	}
	key := string(targetType) + ":" + targetID.String()
	return s.overrides[key], nil
}

// --- Fake Cache ---

type fakeCache struct {
	data      map[string]permissions.Permission
	getErr    error
	setErr    error
	setCalled bool
}

func newFakeCache() *fakeCache {
	return &fakeCache{data: make(map[string]permissions.Permission)}
}

func (c *fakeCache) Get(_ context.Context, userID, channelID uuid.UUID) (permissions.Permission, bool, error) {
	if c.getErr != nil {
		return 0, false, c.getErr
	}
	key := userID.String() + ":" + channelID.String()
	perm, ok := c.data[key]
	return perm, ok, nil
}

func (c *fakeCache) Set(_ context.Context, userID, channelID uuid.UUID, perm permissions.Permission) error {
	c.setCalled = true
	if c.setErr != nil {
		return c.setErr
	}
	key := userID.String() + ":" + channelID.String()
	c.data[key] = perm
	return nil
}

func (c *fakeCache) DeleteByUser(_ context.Context, _ uuid.UUID) error    { return nil }
func (c *fakeCache) DeleteByChannel(_ context.Context, _ uuid.UUID) error { return nil }
func (c *fakeCache) DeleteExact(_ context.Context, _, _ uuid.UUID) error  { return nil }
func (c *fakeCache) DeleteAll(_ context.Context) error                    { return nil }

// --- Tests ---

func TestOwnerBypass(t *testing.T) {
	t.Parallel()
	store := &fakeStore{isOwner: true}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if perm != permissions.AllPermissions {
		t.Errorf("owner permissions = %d, want AllPermissions (%d)", perm, permissions.AllPermissions)
	}
}

func TestManageServerRoleGivesAll(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{{RoleID: roleID, Permissions: permissions.ManageServer}},
		chanInfo:    ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if perm != permissions.AllPermissions {
		t.Errorf("ManageServer permissions = %d, want AllPermissions", perm)
	}
}

func TestRoleUnionOR(t *testing.T) {
	t.Parallel()
	role1 := uuid.New()
	role2 := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: role1, Permissions: permissions.ViewChannels | permissions.SendMessages},
			{RoleID: role2, Permissions: permissions.AddReactions | permissions.EmbedLinks},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	expected := permissions.ViewChannels | permissions.SendMessages | permissions.AddReactions | permissions.EmbedLinks
	if perm != expected {
		t.Errorf("role union = %d, want %d", perm, expected)
	}
}

func TestCategoryDenyOverridesRoleAllow(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()
	categoryID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID, CategoryID: &categoryID},
		overrides: map[string][]Override{
			"category:" + categoryID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Deny: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be denied by category override")
	}
	if !perm.Has(permissions.ViewChannels) {
		t.Error("ViewChannels should still be allowed")
	}
}

func TestChannelOverrideOverridesCategory(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()
	categoryID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID, CategoryID: &categoryID},
		overrides: map[string][]Override{
			"category:" + categoryID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Deny: permissions.SendMessages},
			},
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Allow: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if !perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be re-allowed by channel override")
	}
}

func TestUserOverrideBeatRoleOverride(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
		overrides: map[string][]Override{
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Deny: permissions.SendMessages},
				{PrincipalType: PrincipalUser, PrincipalID: userID, Allow: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if !perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be allowed by user-specific override")
	}
}

func TestDenyWinsAtSameLevel(t *testing.T) {
	t.Parallel()
	role1 := uuid.New()
	role2 := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: role1, Permissions: permissions.ViewChannels},
			{RoleID: role2, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
		overrides: map[string][]Override{
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: role1, Allow: permissions.SendMessages},
				{PrincipalType: PrincipalRole, PrincipalID: role2, Deny: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be denied (deny wins at same level)")
	}
}

func TestEveryoneRoleIncluded(t *testing.T) {
	t.Parallel()
	everyoneRole := uuid.New()
	channelID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: everyoneRole, Permissions: permissions.ViewChannels | permissions.ReadMessageHistory},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	expected := permissions.ViewChannels | permissions.ReadMessageHistory
	if perm != expected {
		t.Errorf("permissions = %d, want %d", perm, expected)
	}
}

func TestNoCategoryOnlyChannelOverrides(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID}, // no category
		overrides: map[string][]Override{
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Deny: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be denied by channel override")
	}
	if !perm.Has(permissions.ViewChannels) {
		t.Error("ViewChannels should still be allowed")
	}
}

func TestCacheHitReturnsCachedValue(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	cache := newFakeCache()
	userID := uuid.New()
	channelID := uuid.New()

	// Pre-populate cache
	cache.data[userID.String()+":"+channelID.String()] = permissions.ViewChannels | permissions.SendMessages

	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	expected := permissions.ViewChannels | permissions.SendMessages
	if perm != expected {
		t.Errorf("cached perm = %d, want %d", perm, expected)
	}

	// Store should not be called
	if store.isOwnerCalled {
		t.Error("Store.IsOwner should not be called on cache hit")
	}
	if store.roleCalled {
		t.Error("Store.RolePermissions should not be called on cache hit")
	}
}

func TestCacheMissComputesAndCaches(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	userID := uuid.New()
	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if perm != permissions.ViewChannels {
		t.Errorf("perm = %d, want ViewChannels", perm)
	}

	// Check cache was populated
	if !cache.setCalled {
		t.Error("Cache.Set should be called on cache miss")
	}
}

func TestCacheGetErrorDegradesToDB(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	cache.getErr = fmt.Errorf("cache unavailable")
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() should not fail on cache error, got: %v", err)
	}
	if perm != permissions.ViewChannels {
		t.Errorf("perm = %d, want ViewChannels", perm)
	}
}

func TestStoreErrorPropagated(t *testing.T) {
	t.Parallel()
	store := &fakeStore{isOwnerErr: fmt.Errorf("db connection lost")}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.Resolve(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("Resolve() should propagate store error")
	}
}

func TestEmptyOverridesLeaveBaseUnchanged(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo:  ChannelInfo{ID: channelID},
		overrides: map[string][]Override{},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	expected := permissions.ViewChannels | permissions.SendMessages
	if perm != expected {
		t.Errorf("perm = %d, want %d (base unchanged)", perm, expected)
	}
}

func TestRolePermissionsError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{roleErr: fmt.Errorf("db error")}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.Resolve(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("Resolve() should propagate role permissions error")
	}
}

func TestChannelInfoError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels},
		},
		chanInfoErr: fmt.Errorf("channel not found"),
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.Resolve(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("Resolve() should propagate channel info error")
	}
}

func TestCategoryOverridesError(t *testing.T) {
	t.Parallel()
	catID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels},
		},
		chanInfo:     ChannelInfo{ID: uuid.New(), CategoryID: &catID},
		overridesErr: fmt.Errorf("overrides query failed"),
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.Resolve(context.Background(), uuid.New(), uuid.New())
	if err == nil {
		t.Fatal("Resolve() should propagate category overrides error")
	}
}

func TestChannelOverridesError(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	// Override the Overrides method to fail on channel override query
	// Since we have no category, the first call is the channel override
	store.overridesErr = fmt.Errorf("channel overrides failed")
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err == nil {
		t.Fatal("Resolve() should propagate channel overrides error")
	}
}

func TestCacheSetError(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	cache.setErr = fmt.Errorf("cache write failed")
	r := NewResolver(store, cache, zerolog.Nop())

	// Should still succeed even if cache set fails
	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() should not fail on cache set error, got: %v", err)
	}
	if perm != permissions.ViewChannels {
		t.Errorf("perm = %d, want ViewChannels", perm)
	}
}

func TestHasPermission(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())
	userID := uuid.New()

	has, err := r.HasPermission(context.Background(), userID, channelID, permissions.ViewChannels)
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if !has {
		t.Error("should have ViewChannels")
	}

	has, err = r.HasPermission(context.Background(), userID, channelID, permissions.ManageRoles)
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if has {
		t.Error("should not have ManageRoles")
	}
}

// --- ResolveServer tests ---

func TestResolveServer_OwnerBypass(t *testing.T) {
	t.Parallel()
	store := &fakeStore{isOwner: true}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.ResolveServer(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveServer() error = %v", err)
	}
	if perm != permissions.AllPermissions {
		t.Errorf("owner permissions = %d, want AllPermissions (%d)", perm, permissions.AllPermissions)
	}
}

func TestResolveServer_ManageServerGivesAll(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ManageServer},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.ResolveServer(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveServer() error = %v", err)
	}
	if perm != permissions.AllPermissions {
		t.Errorf("ManageServer permissions = %d, want AllPermissions", perm)
	}
}

func TestResolveServer_RoleUnion(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels | permissions.SendMessages},
			{RoleID: uuid.New(), Permissions: permissions.AddReactions | permissions.EmbedLinks},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.ResolveServer(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveServer() error = %v", err)
	}

	expected := permissions.ViewChannels | permissions.SendMessages | permissions.AddReactions | permissions.EmbedLinks
	if perm != expected {
		t.Errorf("role union = %d, want %d", perm, expected)
	}
}

func TestResolveServer_NoRoles(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.ResolveServer(context.Background(), uuid.New())
	if err != nil {
		t.Fatalf("ResolveServer() error = %v", err)
	}
	if perm != 0 {
		t.Errorf("no-role permissions = %d, want 0", perm)
	}
}

func TestResolveServer_StoreError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{isOwnerErr: fmt.Errorf("db down")}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.ResolveServer(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("ResolveServer() should propagate store error")
	}
}

func TestResolveServer_RolePermissionsError(t *testing.T) {
	t.Parallel()
	store := &fakeStore{roleErr: fmt.Errorf("db error")}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	_, err := r.ResolveServer(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("ResolveServer() should propagate role permissions error")
	}
}

func TestNoRolesGivesZeroPermissions(t *testing.T) {
	t.Parallel()
	channelID := uuid.New()
	store := &fakeStore{
		roleEntries: nil, // user holds no roles
		chanInfo:    ChannelInfo{ID: channelID},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), uuid.New(), channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if perm != 0 {
		t.Errorf("no-role permissions = %d, want 0", perm)
	}
}

func TestUserDenyBeatsRoleAllow(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels},
		},
		chanInfo: ChannelInfo{ID: channelID},
		overrides: map[string][]Override{
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalRole, PrincipalID: roleID, Allow: permissions.SendMessages},
				{PrincipalType: PrincipalUser, PrincipalID: userID, Deny: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be denied by user-specific override even though role allows it")
	}
	if !perm.Has(permissions.ViewChannels) {
		t.Error("ViewChannels should still be allowed")
	}
}

func TestCategoryUserOverrideOverriddenByChannelUserOverride(t *testing.T) {
	t.Parallel()
	roleID := uuid.New()
	userID := uuid.New()
	channelID := uuid.New()
	categoryID := uuid.New()

	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: roleID, Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
		chanInfo: ChannelInfo{ID: channelID, CategoryID: &categoryID},
		overrides: map[string][]Override{
			"category:" + categoryID.String(): {
				{PrincipalType: PrincipalUser, PrincipalID: userID, Deny: permissions.SendMessages},
			},
			"channel:" + channelID.String(): {
				{PrincipalType: PrincipalUser, PrincipalID: userID, Allow: permissions.SendMessages},
			},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	perm, err := r.Resolve(context.Background(), userID, channelID)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	if !perm.Has(permissions.SendMessages) {
		t.Error("SendMessages should be re-allowed by channel-level user override")
	}
}

func TestHasServerPermission(t *testing.T) {
	t.Parallel()
	store := &fakeStore{
		roleEntries: []RolePermEntry{
			{RoleID: uuid.New(), Permissions: permissions.ViewChannels | permissions.SendMessages},
		},
	}
	cache := newFakeCache()
	r := NewResolver(store, cache, zerolog.Nop())

	has, err := r.HasServerPermission(context.Background(), uuid.New(), permissions.ViewChannels)
	if err != nil {
		t.Fatalf("HasServerPermission() error = %v", err)
	}
	if !has {
		t.Error("should have ViewChannels")
	}

	has, err = r.HasServerPermission(context.Background(), uuid.New(), permissions.ManageRoles)
	if err != nil {
		t.Fatalf("HasServerPermission() error = %v", err)
	}
	if has {
		t.Error("should not have ManageRoles")
	}
}
