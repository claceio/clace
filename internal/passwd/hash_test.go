// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package passwd

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestHashAndVerifyArgon2id(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !IsPasswordHash(hash) || NeedsRehash(hash) {
		t.Fatalf("fresh hash must be recognized and current: %s", hash)
	}
	if ok, err := VerifyPassword(hash, "correct horse"); err != nil || !ok {
		t.Fatalf("verify: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(hash, "wrong"); err != nil || ok {
		t.Fatalf("wrong password: ok=%v err=%v", ok, err)
	}

	// Two hashes of the same password differ (random salt)
	again, _ := HashPassword("correct horse")
	if again == hash {
		t.Fatal("hashes must use a random salt")
	}
}

func TestVerifyBcryptAndRehash(t *testing.T) {
	bhash, err := bcrypt.GenerateFromPassword([]byte("abcd"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !IsPasswordHash(string(bhash)) || !NeedsRehash(string(bhash)) {
		t.Fatalf("bcrypt hash must verify and need a rehash")
	}
	if ok, err := VerifyPassword(string(bhash), "abcd"); err != nil || !ok {
		t.Fatalf("bcrypt verify: ok=%v err=%v", ok, err)
	}
	if ok, err := VerifyPassword(string(bhash), "abce"); err != nil || ok {
		t.Fatalf("bcrypt wrong: ok=%v err=%v", ok, err)
	}

	// Non-default argon2id params are outdated too
	weak := "$argon2id$v=19$m=1024,t=1,p=1$c2FsdHNhbHRzYWx0c2Fs$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if !NeedsRehash(weak) {
		t.Fatal("weaker params must need a rehash")
	}
}

func TestVerifyRejectsBadFormats(t *testing.T) {
	for _, bad := range []string{"", "plaintext", "$argon2id$v=18$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=1,t=1$a$b",
		"$argon2id$v=19$m=1,t=1,p=1$!!$b", "$2a$short"} {
		if IsPasswordHash(bad) {
			t.Errorf("%q must not be a valid hash", bad)
		}
		if _, err := VerifyPassword(bad, "x"); err == nil {
			t.Errorf("%q must fail verification with an error", bad)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	if err := ValidatePassword("short", "alice"); err == nil {
		t.Error("short password accepted")
	}
	if err := ValidatePassword(strings.Repeat("x", 129), "alice"); err == nil {
		t.Error("long password accepted")
	}
	if err := ValidatePassword("AliceAlice", "alicealice"); err == nil {
		t.Error("password equal to username accepted")
	}
	if err := ValidatePassword("goodpassword", "alice"); err != nil {
		t.Errorf("valid password rejected: %v", err)
	}
}

func TestDummyHash(t *testing.T) {
	if h := DummyHash(); h == "" || h != DummyHash() || !IsPasswordHash(h) {
		t.Fatalf("dummy hash must be a stable valid hash: %q", h)
	}
}
