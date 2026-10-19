// SPDX-FileCopyrightText: 2026 The Homelab Control Authors
//
// SPDX-License-Identifier: GPL-3.0-or-later

// Package auth implements local account verification, sessions, CSRF tokens
// and login throttling for the Homelab Control API.
//
// Only the Go standard library is used so that the authentication path has no
// third-party supply-chain dependencies (see docs/adr/0002-api-session-model.md).
package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const (
	// DefaultIterations follows the OWASP Password Storage Cheat Sheet
	// recommendation for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600_000
	// MinIterations is the lowest work factor accepted when verifying a stored
	// hash; weaker hashes are rejected rather than silently accepted.
	MinIterations = 210_000
	saltLen       = 16
	keyLen        = 32
	hashScheme    = "pbkdf2-sha256"
	// MinPasswordLength is enforced when creating or changing passwords.
	MinPasswordLength = 12
)

var b64 = base64.RawStdEncoding

// ErrMalformedHash is returned for stored hashes that cannot be parsed.
var ErrMalformedHash = errors.New("malformed password hash")

// HashPassword derives a PBKDF2-HMAC-SHA256 hash encoded as
// "pbkdf2-sha256$<iterations>$<salt b64>$<key b64>".
func HashPassword(password string, iterations int) (string, error) {
	if iterations < MinIterations {
		return "", fmt.Errorf("iterations %d below minimum %d", iterations, MinIterations)
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, keyLen)
	if err != nil {
		return "", err
	}
	return strings.Join([]string{hashScheme, strconv.Itoa(iterations), b64.EncodeToString(salt), b64.EncodeToString(key)}, "$"), nil
}

type parsedHash struct {
	iterations int
	salt, key  []byte
}

func parseHash(encoded string) (parsedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashScheme {
		return parsedHash{}, ErrMalformedHash
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < MinIterations || iter > 10_000_000 {
		return parsedHash{}, ErrMalformedHash
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil || len(salt) < 8 {
		return parsedHash{}, ErrMalformedHash
	}
	key, err := b64.DecodeString(parts[3])
	if err != nil || len(key) != keyLen {
		return parsedHash{}, ErrMalformedHash
	}
	return parsedHash{iterations: iter, salt: salt, key: key}, nil
}

// ValidateHash reports whether an encoded hash is well formed.
func ValidateHash(encoded string) error {
	_, err := parseHash(encoded)
	return err
}

// VerifyPassword checks a password against an encoded hash in constant time.
func VerifyPassword(password, encoded string) (bool, error) {
	p, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	key, err := pbkdf2.Key(sha256.New, password, p.salt, p.iterations, keyLen)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

// dummyHash is verified for unknown or disabled users so that response timing
// does not reveal whether an account exists.
var dummyHash = func() string {
	h, err := HashPassword("homelab-control-timing-equalizer", DefaultIterations)
	if err != nil {
		panic(err)
	}
	return h
}()
