package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// tokenBytes is the entropy of a session token (256 bits).
const tokenBytes = 32

// GenerateToken creates a new opaque bearer token together with the keyed hash
// that is persisted in the database. The raw token is returned to the client once
// and is never stored.
func GenerateToken(pepper string) (token string, tokenHash string, err error) {
	buffer := make([]byte, tokenBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("failed to generate session token: %w", err)
	}

	token = base64.RawURLEncoding.EncodeToString(buffer)
	return token, HashToken(pepper, token), nil
}

// HashToken derives the stored representation of a token.
// HMAC with a server-side pepper keeps stored hashes useless if the database leaks.
func HashToken(pepper, token string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte(token))
	return hex.EncodeToString(mac.Sum(nil))
}
