// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package passwd

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

// Password hashing for builtin user accounts. Hashes are stored as PHC
// strings so the algorithm and its parameters are self-describing: new
// hashes are argon2id with the OWASP recommended minimum parameters, bcrypt
// hashes ($2a$/$2b$/$2y$, produced by older versions and by `openrun
// password`) are still verified. A caller which verified a password against
// a hash NeedsRehash reports as outdated can store HashPassword of the same
// password to upgrade it in place.
const (
	argon2Memory  uint32 = 19456 // KiB, 19 MiB
	argon2Time    uint32 = 2
	argon2Threads uint8  = 1
	argon2SaltLen        = 16
	argon2KeyLen  uint32 = 32

	argon2idPrefix = "$argon2id$"

	// Password policy (NIST SP 800-63B shape: a length floor, a generous
	// ceiling, no composition rules)
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

var ErrPasswordHashFormat = errors.New("unsupported password hash format")

// HashPassword returns the argon2id PHC hash of the password, with a fresh
// random salt: $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)
	return fmt.Sprintf("%sv=%d$m=%d,t=%d,p=%d$%s$%s", argon2idPrefix, argon2.Version,
		argon2Memory, argon2Time, argon2Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks the password against a stored hash (argon2id PHC or
// bcrypt). A wrong password returns false with a nil error; an unparseable
// hash returns an error
func VerifyPassword(hash, password string) (bool, error) {
	switch {
	case strings.HasPrefix(hash, argon2idPrefix):
		params, salt, key, err := parseArgon2id(hash)
		if err != nil {
			return false, err
		}
		computed := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.threads, uint32(len(key)))
		return subtle.ConstantTimeCompare(computed, key) == 1, nil
	case IsBcryptHash(hash):
		err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
		if err == nil {
			return true, nil
		}
		if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			return false, nil
		}
		return false, err
	}
	return false, ErrPasswordHashFormat
}

// IsPasswordHash reports whether the value is a hash VerifyPassword can check
func IsPasswordHash(hash string) bool {
	if strings.HasPrefix(hash, argon2idPrefix) {
		_, _, _, err := parseArgon2id(hash)
		return err == nil
	}
	if IsBcryptHash(hash) {
		_, err := bcrypt.Cost([]byte(hash))
		return err == nil
	}
	return false
}

// IsBcryptHash reports whether the value has a bcrypt prefix
func IsBcryptHash(hash string) bool {
	return strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$")
}

// NeedsRehash reports whether a stored hash should be replaced by
// HashPassword of the same password: bcrypt, or argon2id with parameters
// other than the current defaults
func NeedsRehash(hash string) bool {
	if !strings.HasPrefix(hash, argon2idPrefix) {
		return true
	}
	params, _, key, err := parseArgon2id(hash)
	if err != nil {
		return true
	}
	return params.memory != argon2Memory || params.time != argon2Time ||
		params.threads != argon2Threads || uint32(len(key)) != argon2KeyLen
}

// ValidatePassword enforces the password policy for a builtin user
func ValidatePassword(password, username string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > MaxPasswordLength {
		return fmt.Errorf("password cannot be longer than %d characters", MaxPasswordLength)
	}
	if strings.EqualFold(password, username) {
		return fmt.Errorf("password cannot be the same as the username")
	}
	return nil
}

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

// DummyHash returns a fixed argon2id hash of a random secret, used to run a
// verification of the same cost for an unknown username so response timing
// does not reveal whether an account exists
func DummyHash() string {
	dummyHashOnce.Do(func() {
		secret, err := GenerateRandomKey(32)
		if err != nil {
			secret = []byte("openrun-dummy-password-hash-seed")
		}
		hash, err := HashPassword(base64.StdEncoding.EncodeToString(secret))
		if err != nil {
			hash = ""
		}
		dummyHash = hash
	})
	return dummyHash
}

type argon2Params struct {
	memory  uint32
	time    uint32
	threads uint8
}

// parseArgon2id decodes $argon2id$v=19$m=..,t=..,p=..$<salt>$<hash>
func parseArgon2id(hash string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, ErrPasswordHashFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 version %q", ErrPasswordHashFormat, parts[2])
	}
	var params argon2Params
	for param := range strings.SplitSeq(parts[3], ",") {
		name, value, ok := strings.Cut(param, "=")
		if !ok {
			return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 params %q", ErrPasswordHashFormat, parts[3])
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 param %s: %v", ErrPasswordHashFormat, name, err)
		}
		switch name {
		case "m":
			params.memory = uint32(n)
		case "t":
			params.time = uint32(n)
		case "p":
			if n == 0 || n > 255 {
				return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 parallelism %d", ErrPasswordHashFormat, n)
			}
			params.threads = uint8(n)
		default:
			return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 param %q", ErrPasswordHashFormat, name)
		}
	}
	if params.memory == 0 || params.time == 0 || params.threads == 0 {
		return argon2Params{}, nil, nil, fmt.Errorf("%w: incomplete argon2 params", ErrPasswordHashFormat)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 salt", ErrPasswordHashFormat)
	}
	key, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(key) == 0 {
		return argon2Params{}, nil, nil, fmt.Errorf("%w: argon2 hash", ErrPasswordHashFormat)
	}
	return params, salt, key, nil
}
