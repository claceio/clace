// Copyright (c) ClaceIO, LLC
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"crypto/rand"
	"math/big"
)

const (
	PASSWORD_CHARS  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789@#%^&*()_-+=<>?/|"
	PASSWORD_LENGTH = 16
	BCRYPT_COST     = 12
)

// GenerateRandomPassword generates a random password
func GenerateRandomPassword() (string, error) {
	charsetLength := len(PASSWORD_CHARS)
	password := make([]byte, PASSWORD_LENGTH)

	for i := 0; i < PASSWORD_LENGTH; i++ {
		randomIndex, err := rand.Int(rand.Reader, big.NewInt(int64(charsetLength)))
		if err != nil {
			return "", err
		}
		password[i] = PASSWORD_CHARS[randomIndex.Int64()]
	}

	return string(password), nil
}
