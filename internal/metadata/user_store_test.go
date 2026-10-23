// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package metadata

import (
	"context"
	"encoding/json/v2"
	"errors"
	"testing"
	"time"

	"github.com/openrundev/openrun/internal/types"
)

func TestBuiltinUserStore(t *testing.T) {
	m, cleanup := setupTestMetadata(t)
	defer cleanup()
	ctx := context.Background()

	if _, err := m.GetBuiltinUser(ctx, "alice"); !errors.Is(err, types.ErrUserNotFound) {
		t.Fatalf("missing user: %v", err)
	}
	alice := &types.BuiltinUser{Id: "usr_1", Username: "alice", PasswordHash: "$2a$10$x", Groups: []string{"dev"}, CreatedBy: "admin"}
	if err := m.CreateBuiltinUser(ctx, alice); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateBuiltinUser(ctx, &types.BuiltinUser{Id: "usr_2", Username: "alice", PasswordHash: "x"}); !errors.Is(err, ErrUserExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := m.CreateBuiltinUser(ctx, &types.BuiltinUser{Id: "usr_2", Username: "Alice", PasswordHash: "x"}); !errors.Is(err, ErrUserExists) {
		t.Fatalf("case-variant duplicate must be refused: %v", err)
	}
	bob := &types.BuiltinUser{Id: "usr_3", Username: "bob", PasswordHash: "h"}
	if err := m.CreateBuiltinUser(ctx, bob); err != nil {
		t.Fatal(err)
	}

	got, err := m.GetBuiltinUser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Id != "usr_1" || got.CredentialVersion != 1 || len(got.Groups) != 1 || got.Groups[0] != "dev" ||
		got.CreatedBy != "admin" || got.Disabled() || got.PasswordUpdatedAt.IsZero() || got.FailedAttempts != 0 {
		t.Fatalf("unexpected row: %+v", got)
	}
	if bobRow, _ := m.GetBuiltinUser(ctx, "bob"); bobRow.Groups == nil || len(bobRow.Groups) != 0 {
		t.Fatalf("groups must be an empty list, got %#v", bobRow.Groups)
	}

	// Import: insert-if-absent
	inserted, err := m.ImportBuiltinUser(ctx, &types.BuiltinUser{Id: "usr_9", Username: "alice", PasswordHash: "other"})
	if err != nil || inserted {
		t.Fatalf("import of an existing user must be skipped: %v %v", inserted, err)
	}
	inserted, err = m.ImportBuiltinUser(ctx, &types.BuiltinUser{Id: "usr_4", Username: "carol", PasswordHash: "c", CreatedBy: "config"})
	if err != nil || !inserted {
		t.Fatalf("import of a new user: %v %v", inserted, err)
	}

	// Update
	now := time.Now().UTC().Truncate(time.Second)
	got.Groups = []string{"dev", "qa"}
	got.CredentialVersion++
	got.DisabledAt = &now
	got.Email = "alice@example.com"
	if err := m.UpdateBuiltinUser(ctx, got); err != nil {
		t.Fatal(err)
	}
	got, _ = m.GetBuiltinUser(ctx, "alice")
	if got.CredentialVersion != 2 || len(got.Groups) != 2 || !got.Disabled() || got.Email != "alice@example.com" {
		t.Fatalf("update not applied: %+v", got)
	}
	if err := m.UpdateBuiltinUser(ctx, &types.BuiltinUser{Id: "nope", Username: "nope"}); !errors.Is(err, types.ErrUserNotFound) {
		t.Fatalf("update of a missing row: %v", err)
	}

	// Login bookkeeping: failures count atomically, the threshold locks
	bobRow, _ := m.GetBuiltinUser(ctx, "bob")
	for i := 1; i <= 2; i++ {
		if locked, err := m.RecordBuiltinLoginFailure(ctx, bobRow.Id, 3, time.Now().Add(time.Hour)); err != nil || locked {
			t.Fatalf("failure %d: %v locked=%v", i, err, locked)
		}
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if bobRow.FailedAttempts != 2 || bobRow.Locked(time.Now()) {
		t.Fatalf("failures not counted: %+v", bobRow)
	}
	if locked, err := m.RecordBuiltinLoginFailure(ctx, bobRow.Id, 3, time.Now().Add(time.Hour)); err != nil || !locked {
		t.Fatalf("third failure must lock: %v locked=%v", err, locked)
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if bobRow.FailedAttempts != 0 || !bobRow.Locked(time.Now()) {
		t.Fatalf("lock not applied: %+v", bobRow)
	}
	if err := m.UnlockBuiltinUser(ctx, bobRow.Id); err != nil {
		t.Fatal(err)
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if bobRow.Locked(time.Now()) {
		t.Fatalf("unlock must clear the lock: %+v", bobRow)
	}
	// Success with a rehash: written only while the row still holds the
	// verified hash, so an administrator's password set in between wins
	if err := m.RecordBuiltinLoginSuccess(ctx, bobRow.Id, "stale-hash", "$argon2id$lost"); err != nil {
		t.Fatal(err)
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if bobRow.PasswordHash != "h" || bobRow.LastLoginAt == nil {
		t.Fatalf("rehash against a stale hash must be dropped, login still recorded: %+v", bobRow)
	}
	if err := m.RecordBuiltinLoginSuccess(ctx, bobRow.Id, "h", "$argon2id$new"); err != nil {
		t.Fatal(err)
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if bobRow.PasswordHash != "$argon2id$new" || bobRow.CredentialVersion != 1 || bobRow.RowVersion != 1 {
		t.Fatalf("rehash not recorded (versions unchanged): %+v", bobRow)
	}

	// Optimistic concurrency: a stale row cannot overwrite a newer update
	stale, _ := m.GetBuiltinUser(ctx, "bob")
	current, _ := m.GetBuiltinUser(ctx, "bob")
	now2 := time.Now().UTC().Truncate(time.Second)
	current.DisabledAt = &now2
	current.CredentialVersion++
	if err := m.UpdateBuiltinUser(ctx, current); err != nil {
		t.Fatal(err)
	}
	stale.Groups = []string{"late"}
	if err := m.UpdateBuiltinUser(ctx, stale); !errors.Is(err, ErrUserConflict) {
		t.Fatalf("stale update must conflict: %v", err)
	}
	bobRow, _ = m.GetBuiltinUser(ctx, "bob")
	if !bobRow.Disabled() || bobRow.CredentialVersion != 2 || len(bobRow.Groups) != 0 || bobRow.RowVersion != 2 {
		t.Fatalf("the disable must survive the stale groups update: %+v", bobRow)
	}
	bobRow.DisabledAt = nil
	if err := m.UpdateBuiltinUser(ctx, bobRow); err != nil {
		t.Fatal(err)
	}

	users, err := m.ListBuiltinUsers(ctx)
	if err != nil || len(users) != 3 || users[0].Username != "alice" || users[1].Username != "bob" || users[2].Username != "carol" {
		t.Fatalf("list: %v %+v", err, users)
	}

	// Deleting a user revokes its live credentials in the same transaction;
	// the identity row stays usable for a later user of the same name and
	// other principals' credentials are untouched
	for _, name := range []string{"alice", "bob"} {
		if err := m.CreateIdentity(ctx, &types.Identity{Id: "idn_" + name, Provider: "builtin", StableSubject: name,
			PrincipalName: "builtin:" + name}); err != nil {
			t.Fatal(err)
		}
		if err := m.CreateCredential(ctx, &types.Credential{Id: "cred_" + name, SecretHash: "s" + name, Type: types.CredentialTypePAT,
			IdentityId: "idn_" + name, Resources: []string{"rest"}, CreatedBy: "admin"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.DeleteBuiltinUser(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteBuiltinUser(ctx, "bob"); !errors.Is(err, types.ErrUserNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	cred, idn, err := m.GetCredentialWithIdentity(ctx, "cred_bob")
	if err != nil || cred.RevokedAt == nil || cred.RevocationReason != "user deleted" || idn.DisabledAt != nil {
		t.Fatalf("credential must be revoked and the identity kept: %v %+v %+v", err, cred, idn)
	}
	cred, _, err = m.GetCredentialWithIdentity(ctx, "cred_alice")
	if err != nil || cred.RevokedAt != nil {
		t.Fatalf("other users' credentials must be kept: %v %+v", err, cred)
	}
	// A user without an identity (never issued a credential) deletes fine
	if err := m.DeleteBuiltinUser(ctx, "carol"); err != nil {
		t.Fatal(err)
	}
}

func TestStripBuiltinAuthEntries(t *testing.T) {
	// No section: unchanged
	doc := []byte(`{"version_id":"v1","rbac":{"grants":[]},"entries":{"git_auth":{"gh":{"user_id":"x"}}}}`)
	out, users, dropped, changed, err := stripBuiltinAuthEntries(doc)
	if err != nil || changed || users != nil || dropped != nil || string(out) != string(doc) {
		t.Fatalf("no section: %v %v %v %s", err, changed, users, out)
	}

	// Defaults dropped, modified default and other users kept, other fields survive
	doc = []byte(`{"version_id":"v1","rbac":{"grants":[]},"settings":{"system":{"default_domain":"x"}},` +
		`"entries":{"git_auth":{"gh":{"user_id":"x"}},"builtin_auth":{` +
		`"test1":{"password":"` + legacyDefaultUserHashes["test1"] + `","groups":[]},` +
		`"test2":{"password":"changed","groups":["ops"]},` +
		`"alice":{"password":"$2a$10$h","groups":["dev","qa"]}}}}`)
	out, users, dropped, changed, err = stripBuiltinAuthEntries(doc)
	if err != nil || !changed {
		t.Fatalf("strip: %v %v", err, changed)
	}
	if len(dropped) != 1 || dropped[0] != "test1" {
		t.Fatalf("test1 must be dropped: %v", dropped)
	}
	if len(users) != 2 || users["test2"].Password != "changed" || users["test2"].Groups[0] != "ops" ||
		users["alice"].Password != "$2a$10$h" || len(users["alice"].Groups) != 2 {
		t.Fatalf("users: %+v", users)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	entries := parsed["entries"].(map[string]any)
	if entries["builtin_auth"] != nil || entries["git_auth"] == nil || parsed["settings"] == nil || parsed["version_id"] != "v1" {
		t.Fatalf("section must be removed and the rest kept: %s", out)
	}

	// Only section: entries key removed entirely
	out, _, _, _, err = stripBuiltinAuthEntries([]byte(`{"version_id":"v1","entries":{"builtin_auth":{"a":{"password":"p"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	parsed = nil
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["entries"]; ok {
		t.Fatalf("empty entries must be dropped: %s", out)
	}
}

func TestMigrateBuiltinUsersToTable(t *testing.T) {
	m, cleanup := setupTestMetadata(t)
	defer cleanup()
	ctx := context.Background()

	// Simulate a version 32 store: config with builtin_auth entries, a
	// history snapshot carrying them, and no users table
	live := `{"version_id":"v2","rbac":{"grants":[{"users":["*"],"roles":["openrun-user"]}]},"entries":{"builtin_auth":{` +
		`"test1":{"password":"` + legacyDefaultUserHashes["test1"] + `","groups":[]},` +
		`"test2":{"password":"` + legacyDefaultUserHashes["test2"] + `","groups":[]},` +
		`"alice":{"password":"$2a$10$alicehash","groups":["dev"]}}}}`
	if _, err := m.db.ExecContext(ctx, `update config set config = ?`, live); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.ExecContext(ctx, `insert into config_history (version_id, user_id, update_time, config) values (?, ?, ?, ?)`,
		"v2", "admin", time.Now(), live); err != nil {
		t.Fatal(err)
	}
	if _, err := m.db.ExecContext(ctx, `drop table builtin_users`); err != nil {
		t.Fatal(err)
	}

	tx, err := m.BeginTransaction(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.migrateBuiltinUsersToTable(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	users, err := m.ListBuiltinUsers(ctx)
	if err != nil || len(users) != 1 || users[0].Username != "alice" || users[0].PasswordHash != "$2a$10$alicehash" ||
		users[0].Groups[0] != "dev" || users[0].CreatedBy != "migration" {
		t.Fatalf("only alice must be migrated: %v %+v", err, users)
	}
	config, err := m.GetConfig()
	if err != nil || config.Entries["builtin_auth"] != nil || len(config.RBAC.Grants) != 1 || config.VersionId != "v2" {
		t.Fatalf("live config must lose the section and keep the rest: %v %+v", err, config)
	}
	hist, err := m.GetConfigVersion(ctx, "v2")
	if err != nil || hist.Entries["builtin_auth"] != nil || len(hist.RBAC.Grants) != 1 {
		t.Fatalf("history must be scrubbed: %v %+v", err, hist)
	}

	// Re-run: no-op
	tx, _ = m.BeginTransaction(ctx)
	if err := m.migrateBuiltinUsersToTable(ctx, tx); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	if users, _ = m.ListBuiltinUsers(ctx); len(users) != 1 {
		t.Fatalf("re-run must not duplicate users: %+v", users)
	}
}
