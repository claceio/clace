// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/base64"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/metadata"
	"github.com/openrundev/openrun/internal/passwd"
	"github.com/openrundev/openrun/internal/testutil"
	"github.com/openrundev/openrun/internal/types"
	"golang.org/x/crypto/bcrypt"
)

func basicAuthHeader(user, pass string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass))
}

// memoryUserStore is an in-memory userStore for tests of servers without a
// metadata database
type memoryUserStore struct {
	mu    sync.Mutex
	users map[string]*types.BuiltinUser
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{users: map[string]*types.BuiltinUser{}}
}

func (s *memoryUserStore) GetBuiltinUser(_ context.Context, username string) (*types.BuiltinUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[username]
	if !ok {
		return nil, types.ErrUserNotFound
	}
	copied := *u
	return &copied, nil
}

func (s *memoryUserStore) ListBuiltinUsers(context.Context) ([]types.BuiltinUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	users := make([]types.BuiltinUser, 0, len(s.users))
	for _, u := range s.users {
		users = append(users, *u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Username < users[j].Username })
	return users, nil
}

func (s *memoryUserStore) CreateBuiltinUser(_ context.Context, user *types.BuiltinUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[user.Username]; ok {
		return types.ErrUserNotFound // never used by the tests through this path
	}
	now := time.Now()
	user.CreateTime, user.UpdateTime, user.PasswordUpdatedAt = now, now, now
	if user.CredentialVersion == 0 {
		user.CredentialVersion = 1
	}
	user.RowVersion = 1
	copied := *user
	s.users[user.Username] = &copied
	return nil
}

func (s *memoryUserStore) UpdateBuiltinUser(_ context.Context, user *types.BuiltinUser) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.users[user.Username]
	if !ok {
		return types.ErrUserNotFound
	}
	if current.RowVersion != user.RowVersion {
		return metadata.ErrUserConflict
	}
	user.RowVersion++
	copied := *user
	// login counters are not account fields
	copied.FailedAttempts, copied.LockedUntil, copied.LastLoginAt = current.FailedAttempts, current.LockedUntil, current.LastLoginAt
	s.users[user.Username] = &copied
	return nil
}

func (s *memoryUserStore) DeleteBuiltinUser(_ context.Context, username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[username]; !ok {
		return types.ErrUserNotFound
	}
	delete(s.users, username)
	return nil
}

func (s *memoryUserStore) ImportBuiltinUser(ctx context.Context, user *types.BuiltinUser) (bool, error) {
	s.mu.Lock()
	_, exists := s.users[user.Username]
	s.mu.Unlock()
	if exists {
		return false, nil
	}
	return true, s.CreateBuiltinUser(ctx, user)
}

func (s *memoryUserStore) byId(id string) *types.BuiltinUser {
	for _, u := range s.users {
		if u.Id == id {
			return u
		}
	}
	return nil
}

func (s *memoryUserStore) UnlockBuiltinUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.byId(id); u != nil {
		u.FailedAttempts, u.LockedUntil = 0, nil
	}
	return nil
}

func (s *memoryUserStore) RecordBuiltinLoginSuccess(_ context.Context, id string, verifiedHash, rehash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.byId(id)
	if u == nil {
		return nil
	}
	now := time.Now()
	u.FailedAttempts, u.LockedUntil, u.LastLoginAt = 0, nil, &now
	if rehash != "" && u.PasswordHash == verifiedHash {
		u.PasswordHash = rehash
	}
	return nil
}

func (s *memoryUserStore) RecordBuiltinLoginFailure(_ context.Context, id string, threshold int, lockUntil time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.byId(id)
	if u == nil {
		return false, nil
	}
	u.FailedAttempts++
	if u.FailedAttempts >= threshold {
		u.FailedAttempts, u.LockedUntil = 0, &lockUntil
		return true, nil
	}
	return false, nil
}

// newTestUserManager builds a UserManager over an in-memory store seeded
// with the users, without the login failure delay
func newTestUserManager(t *testing.T, users ...types.BuiltinUser) (*UserManager, *memoryUserStore) {
	t.Helper()
	store := newMemoryUserStore()
	for i := range users {
		u := users[i]
		if u.Id == "" {
			u.Id = "usr_" + u.Username
		}
		if u.PasswordHash == "" {
			u.PasswordHash = "$2a$04$unusedunusedunusedunusedunusedunusedunusedunusedunus"
		}
		if u.Groups == nil {
			u.Groups = []string{}
		}
		if err := store.CreateBuiltinUser(context.Background(), &u); err != nil {
			t.Fatal(err)
		}
	}
	manager := NewUserManager(testutil.TestLogger(), store)
	manager.failDelay = 0
	return manager, store
}

// testUser is a seeded user row for newTestUserManager; the password is
// bcrypt hashed at the minimum cost to keep tests fast
func testUser(t *testing.T, username, password string, groups ...string) types.BuiltinUser {
	t.Helper()
	u := types.BuiltinUser{Username: username, Groups: groups}
	if groups == nil {
		u.Groups = []string{}
	}
	if password != "" {
		u.PasswordHash = hashPassword(t, password)
	}
	return u
}

func hashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(hash)
}

// addDBTestUser creates a user row in the server's metadata database and
// returns it; for tests built on newApplyTestServer
func addDBTestUser(t *testing.T, server *Server, username, password string, groups ...string) *types.BuiltinUser {
	t.Helper()
	u := testUser(t, username, password, groups...)
	u.Id = "usr_" + username
	u.CreatedBy = "test"
	if u.PasswordHash == "" {
		u.PasswordHash = hashPassword(t, "unused-"+username)
	}
	if err := server.db.CreateBuiltinUser(context.Background(), &u); err != nil {
		t.Fatalf("create test user %s: %v", username, err)
	}
	server.users.invalidate(username)
	return &u
}

func TestUserManagerAuthenticate(t *testing.T) {
	auth, store := newTestUserManager(t,
		testUser(t, "alice", "pw1", "dev", "qa"),
		testUser(t, "bob", "pw2"))

	userId, groups, ok := auth.authenticate(basicAuthHeader("alice", "pw1"))
	testutil.AssertEqualsBool(t, "ok", true, ok)
	testutil.AssertEqualsString(t, "userId", "builtin:alice", userId)
	testutil.AssertEqualsInt(t, "groups", 2, len(groups))
	testutil.AssertEqualsString(t, "group", "dev", groups[0])

	// A bcrypt hash is upgraded to argon2id on the successful login; the
	// cached header keeps working and the credential version is unchanged
	stored, _ := store.GetBuiltinUser(context.Background(), "alice")
	if !passwd.IsPasswordHash(stored.PasswordHash) || passwd.NeedsRehash(stored.PasswordHash) || stored.CredentialVersion != 1 {
		t.Fatalf("password must be rehashed in place: %+v", stored)
	}
	if stored.LastLoginAt == nil {
		t.Fatal("last login must be recorded")
	}
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "pw1")); !ok {
		t.Fatal("cached header must authenticate")
	}
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "pw1")); !ok {
		t.Fatal("rehashed password must authenticate")
	}

	// Groups default to an empty list
	userId, groups, ok = auth.authenticate(basicAuthHeader("bob", "pw2"))
	testutil.AssertEqualsBool(t, "ok", true, ok)
	testutil.AssertEqualsString(t, "userId", "builtin:bob", userId)
	testutil.AssertEqualsInt(t, "groups", 0, len(groups))

	_, _, ok = auth.authenticate(basicAuthHeader("alice", "wrong"))
	testutil.AssertEqualsBool(t, "wrong password", false, ok)
	stored, _ = store.GetBuiltinUser(context.Background(), "alice")
	testutil.AssertEqualsInt(t, "failed attempts", 1, stored.FailedAttempts)

	_, _, ok = auth.authenticate(basicAuthHeader("unknown", "pw1"))
	testutil.AssertEqualsBool(t, "unknown user", false, ok)
	_, _, ok = auth.authenticate(basicAuthHeader("Alice", "pw1"))
	testutil.AssertEqualsBool(t, "username is case sensitive", false, ok)

	_, _, ok = auth.authenticate("")
	testutil.AssertEqualsBool(t, "empty header", false, ok)
	_, _, ok = auth.authenticate("Basic not-base64!!!")
	testutil.AssertEqualsBool(t, "bad header", false, ok)

	// A nil manager (a server without users) authenticates nobody
	var none *UserManager
	if _, _, ok := none.authenticate(basicAuthHeader("alice", "pw1")); ok {
		t.Fatal("nil manager must refuse")
	}
	if _, ok := none.groups("alice"); ok {
		t.Fatal("nil manager has no users")
	}
}

func TestUserManagerCachedLoginStamp(t *testing.T) {
	auth, store := newTestUserManager(t, testUser(t, "alice", "pw1"))
	clock := time.Now().UTC()
	auth.now = func() time.Time { return clock }
	header := basicAuthHeader("alice", "pw1")

	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("login must succeed")
	}
	first, _ := store.GetBuiltinUser(context.Background(), "alice")
	if first.LastLoginAt == nil {
		t.Fatal("last login must be recorded")
	}
	// A second sign-in with the cached header right away does not write
	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("cached login must succeed")
	}
	again, _ := store.GetBuiltinUser(context.Background(), "alice")
	if !again.LastLoginAt.Equal(*first.LastLoginAt) {
		t.Fatalf("stamp must not advance within the granularity: %v -> %v", first.LastLoginAt, again.LastLoginAt)
	}
	// A cached sign-in after the granularity advances the stamp, keeps the
	// cached header (no re-verification: the row's hash is unchanged and
	// the header stays in the cache) and keeps the cached row
	clock = clock.Add(2 * lastLoginGranularity)
	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("cached login must succeed")
	}
	later, _ := store.GetBuiltinUser(context.Background(), "alice")
	if !later.LastLoginAt.After(*first.LastLoginAt) {
		t.Fatalf("stamp must advance for a cached sign-in: %v -> %v", first.LastLoginAt, later.LastLoginAt)
	}
	auth.mu.RLock()
	cachedRow, rowOk := auth.rows["alice"]
	cachedHeaders := len(auth.authCache)
	auth.mu.RUnlock()
	if !rowOk || cachedRow.user.LastLoginAt == nil || !cachedRow.user.LastLoginAt.Equal(clock) {
		t.Fatalf("cached row must carry the new stamp: %+v", cachedRow.user)
	}
	testutil.AssertEqualsInt(t, "cached headers kept", 1, cachedHeaders)
	// The in-memory stamp bounds the writes: the next sign-in within the
	// granularity (the row cache is still fresh) does not write
	clock = clock.Add(lastLoginGranularity / 2)
	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("cached login must succeed")
	}
	final, _ := store.GetBuiltinUser(context.Background(), "alice")
	if !final.LastLoginAt.Equal(*later.LastLoginAt) {
		t.Fatalf("stamp must not advance within the granularity: %v -> %v", later.LastLoginAt, final.LastLoginAt)
	}
}

func TestUserManagerDisableAndVersion(t *testing.T) {
	auth, store := newTestUserManager(t, testUser(t, "alice", "pw1", "dev"))
	header := basicAuthHeader("alice", "pw1")
	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("login")
	}

	// Disabling the user (on another node: the row changes and the cache is
	// invalidated) refuses the cached header and drops all authority
	row, _ := store.GetBuiltinUser(context.Background(), "alice")
	now := time.Now()
	row.DisabledAt = &now
	row.CredentialVersion++
	if err := store.UpdateBuiltinUser(context.Background(), row); err != nil {
		t.Fatal(err)
	}
	auth.invalidate("alice")
	if _, _, ok := auth.authenticate(header); ok {
		t.Fatal("disabled user must not authenticate")
	}
	if _, ok := auth.groups("alice"); ok {
		t.Fatal("disabled user must have no groups")
	}
	if _, ok := auth.lookup("alice"); !ok {
		t.Fatal("lookup still finds the row")
	}

	// Re-enable with a new version: the old cached header entry (version 1)
	// is stale, a fresh verification succeeds
	row.DisabledAt = nil
	row.CredentialVersion++
	_ = store.UpdateBuiltinUser(context.Background(), row)
	auth.invalidate("alice")
	if _, groups, ok := auth.authenticate(header); !ok || len(groups) != 1 {
		t.Fatal("enabled user must authenticate")
	}

	// A password change on another node without invalidation: the TTL
	// cache still holds the old row, but the header cache is keyed by the
	// version it saw, so once the row reloads the header is re-verified
	row, _ = store.GetBuiltinUser(context.Background(), "alice")
	row.PasswordHash = hashPassword(t, "pw2")
	row.CredentialVersion++
	_ = store.UpdateBuiltinUser(context.Background(), row)
	auth.invalidate("alice")
	if _, _, ok := auth.authenticate(header); ok {
		t.Fatal("old password must be refused after the change")
	}
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "pw2")); !ok {
		t.Fatal("new password must work")
	}
}

func TestUserManagerRecreatedUser(t *testing.T) {
	// A deleted and recreated user of the same name is a new row: the old
	// cached header must not authenticate against it, even though both rows
	// have credential version 1
	auth, store := newTestUserManager(t, testUser(t, "alice", "oldpassword"))
	header := basicAuthHeader("alice", "oldpassword")
	if _, _, ok := auth.authenticate(header); !ok {
		t.Fatal("login")
	}
	if err := store.DeleteBuiltinUser(context.Background(), "alice"); err != nil {
		t.Fatal(err)
	}
	fresh := testUser(t, "alice", "newpassword")
	fresh.Id = "usr_alice2"
	if err := store.CreateBuiltinUser(context.Background(), &fresh); err != nil {
		t.Fatal(err)
	}
	// Missed notification: only the row cache TTL expires
	auth.now = func() time.Time { return time.Now().Add(userCacheTTL + time.Second) }
	if _, _, ok := auth.authenticate(header); ok {
		t.Fatal("old password must not authenticate against the recreated user")
	}
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "newpassword")); !ok {
		t.Fatal("new password must authenticate")
	}
}

func TestUserManagerLockout(t *testing.T) {
	auth, store := newTestUserManager(t, testUser(t, "alice", "pw1"))
	for i := 0; i < loginLockThreshold; i++ {
		if _, _, ok := auth.authenticate(basicAuthHeader("alice", "wrong")); ok {
			t.Fatal("wrong password accepted")
		}
	}
	row, _ := store.GetBuiltinUser(context.Background(), "alice")
	if !row.Locked(time.Now()) || row.FailedAttempts != 0 {
		t.Fatalf("account must be locked after %d failures: %+v", loginLockThreshold, row)
	}
	// Locked: even the right password is refused, without touching the counters
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "pw1")); ok {
		t.Fatal("locked account must refuse the right password")
	}
	// Lock expiry (the manager's clock moves past the deadline)
	auth.now = func() time.Time { return time.Now().Add(loginLockDuration + time.Minute) }
	auth.invalidate("alice")
	if _, _, ok := auth.authenticate(basicAuthHeader("alice", "pw1")); !ok {
		t.Fatal("expired lock must allow login")
	}
	row, _ = store.GetBuiltinUser(context.Background(), "alice")
	if row.LockedUntil != nil || row.FailedAttempts != 0 {
		t.Fatalf("success must clear the lock: %+v", row)
	}
}

func TestValidateUsername(t *testing.T) {
	for _, valid := range []string{"alice", "alice@example.com", "a_b-c.d", "user1", "Alice+ops", "1abc"} {
		if err := validateUsername(valid); err != nil {
			t.Errorf("expected %q to be valid: %s", valid, err)
		}
	}
	for _, invalid := range []string{"", "a:b", "a b", "a\tb", "a\nb", "_lead", ".lead", "a/b",
		"admin", "Admin", "anonymous", "openrun-bot", "a" + string(make([]byte, 64))} {
		if err := validateUsername(invalid); err == nil {
			t.Errorf("expected %q to be invalid", invalid)
		}
	}
}

func TestServerUserCRUD(t *testing.T) {
	server, db, ctx := newApplyTestServer(t)
	defer db.Close()

	if _, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Password: "alicepass", Groups: []string{"dev"}}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Password: "alicepass"}, false); err == nil {
		t.Fatal("duplicate user must fail")
	}
	if _, err := server.CreateUpdateUser(ctx, "bob", types.UserUpdateRequest{Password: "short"}, false); err == nil {
		t.Fatal("password policy must apply")
	}
	if _, err := server.CreateUpdateUser(ctx, "bob", types.UserUpdateRequest{}, false); err == nil {
		t.Fatal("password required on create")
	}
	if _, err := server.CreateUpdateUser(ctx, "admin", types.UserUpdateRequest{Password: "adminpass"}, false); err == nil {
		t.Fatal("reserved username must be refused")
	}
	if _, err := server.CreateUpdateUser(ctx, "nosuch", types.UserUpdateRequest{Password: "somepass"}, true); err == nil {
		t.Fatal("update of a missing user must fail")
	}

	users, err := server.ListUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Username != "alice" || users[0].Groups[0] != "dev" || users[0].CreatedBy != "admin" {
		t.Fatalf("list: %v %+v", err, users)
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("alice", "alicepass")); !ok {
		t.Fatal("created user must authenticate")
	}
	row, _ := db.GetBuiltinUser(ctx, "alice")
	if passwd.NeedsRehash(row.PasswordHash) {
		t.Fatalf("new users get the current hash algorithm: %s", row.PasswordHash)
	}

	// Password change keeps groups and bumps the version; groups change keeps the password
	if updated, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Password: "newpassword"}, true); err != nil || !updated {
		t.Fatal(err)
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("alice", "alicepass")); ok {
		t.Fatal("old password must stop working")
	}
	if _, groups, ok := server.users.authenticate(basicAuthHeader("alice", "newpassword")); !ok || len(groups) != 1 {
		t.Fatal("new password must work with the groups kept")
	}
	row, _ = db.GetBuiltinUser(ctx, "alice")
	testutil.AssertEqualsInt(t, "version", 2, row.CredentialVersion)
	if _, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Groups: []string{}}, true); err != nil {
		t.Fatal(err)
	}
	if _, groups, ok := server.users.authenticate(basicAuthHeader("alice", "newpassword")); !ok || len(groups) != 0 {
		t.Fatalf("groups must be cleared, password kept: %v %v", groups, ok)
	}

	// Disable / enable
	disabled := true
	if _, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Disabled: &disabled}, true); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("alice", "newpassword")); ok {
		t.Fatal("disabled user must not authenticate")
	}
	info, err := server.GetUser(ctx, "alice")
	if err != nil || !info.Disabled {
		t.Fatalf("show must report disabled: %v %+v", err, info)
	}
	disabled = false
	if _, err := server.CreateUpdateUser(ctx, "alice", types.UserUpdateRequest{Disabled: &disabled, Unlock: true}, true); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("alice", "newpassword")); !ok {
		t.Fatal("enabled user must authenticate")
	}

	if err := server.DeleteUser(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := server.DeleteUser(ctx, "alice"); err == nil {
		t.Fatal("second delete must fail")
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("alice", "newpassword")); ok {
		t.Fatal("deleted user must not authenticate")
	}
	if _, err := server.GetUser(ctx, "alice"); err == nil {
		t.Fatal("show of a deleted user must fail")
	}
}

func TestImportStaticBuiltinUsers(t *testing.T) {
	server, db, ctx := newApplyTestServer(t)
	defer db.Close()
	addDBTestUser(t, server, "existing", "rowpass", "rowgroup")
	server.staticConfig.BuiltinAuth = map[string]types.BuiltinAuthEntry{
		"existing": {Password: hashPassword(t, "configpass"), Groups: []string{"configgroup"}},
		"imported": {Password: hashPassword(t, "importpass"), Groups: []string{"static-group"}},
		"broken":   {Password: "not-a-hash"},
	}
	server.importStaticBuiltinUsers(ctx)
	server.importStaticBuiltinUsers(ctx) // idempotent

	users, err := db.ListBuiltinUsers(ctx)
	if err != nil || len(users) != 2 {
		t.Fatalf("existing + imported expected: %v %+v", err, users)
	}
	// The existing row wins over the config entry
	if _, groups, ok := server.users.authenticate(basicAuthHeader("existing", "rowpass")); !ok || groups[0] != "rowgroup" {
		t.Fatalf("existing row must be kept: %v %v", groups, ok)
	}
	if _, _, ok := server.users.authenticate(basicAuthHeader("existing", "configpass")); ok {
		t.Fatal("config password must not replace the row")
	}
	if _, groups, ok := server.users.authenticate(basicAuthHeader("imported", "importpass")); !ok || groups[0] != "static-group" {
		t.Fatalf("imported user must authenticate: %v %v", groups, ok)
	}
	imported, _ := db.GetBuiltinUser(ctx, "imported")
	testutil.AssertEqualsString(t, "created by", "config", imported.CreatedBy)
}
