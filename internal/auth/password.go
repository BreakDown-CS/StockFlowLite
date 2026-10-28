package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
)

// Password hashing parameters. PBKDF2-HMAC-SHA256 is provided by the Go standard
// library (crypto/pbkdf2), so no third-party dependency is required.
const (
	// DefaultIterations follows the OWASP recommendation for PBKDF2-HMAC-SHA256.
	DefaultIterations = 600_000
	// MinIterations is the lowest work factor the verifier will accept.
	MinIterations  = 100_000
	MinPasswordLen = 8

	saltLength = 16
	keyLength  = 32
	hashPrefix = "pbkdf2-sha256"
	hashFormat = "v=1"
)

var hashEncoding = base64.RawStdEncoding

// Hasher derives and verifies password hashes.
type Hasher struct {
	iterations int
}

// NewHasher builds a Hasher, falling back to the default work factor when the
// configured iteration count is below the accepted minimum.
func NewHasher(iterations int) *Hasher {
	if iterations < MinIterations {
		iterations = DefaultIterations
	}
	return &Hasher{iterations: iterations}
}

// Iterations exposes the configured work factor (useful for logging without secrets).
func (h *Hasher) Iterations() int {
	return h.iterations
}

// Hash derives a salted, versioned hash:
// pbkdf2-sha256$v=1$i=<iterations>$<salt>$<key>
func (h *Hasher) Hash(password string) (string, error) {
	if password == "" {
		return "", domain.ErrWeakPassword
	}

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate password salt: %w", err)
	}

	key, err := pbkdf2.Key(sha256.New, password, salt, h.iterations, keyLength)
	if err != nil {
		return "", fmt.Errorf("failed to derive password hash: %w", err)
	}

	return fmt.Sprintf("%s$%s$i=%d$%s$%s",
		hashPrefix,
		hashFormat,
		h.iterations,
		hashEncoding.EncodeToString(salt),
		hashEncoding.EncodeToString(key),
	), nil
}

// Verify reports whether the password matches the stored hash using a
// constant-time comparison. Unknown formats (for example the seeded
// placeholder identities) never match.
func (h *Hasher) Verify(passwordHash, password string) bool {
	params, ok := parsePasswordHash(passwordHash)
	if !ok || password == "" {
		return false
	}

	key, err := pbkdf2.Key(sha256.New, password, params.salt, params.iterations, len(params.key))
	if err != nil {
		return false
	}

	return subtle.ConstantTimeCompare(key, params.key) == 1
}

// NeedsRehash reports whether a stored hash uses outdated parameters.
func (h *Hasher) NeedsRehash(passwordHash string) bool {
	params, ok := parsePasswordHash(passwordHash)
	if !ok {
		return true
	}
	return params.iterations < h.iterations
}

// ValidatePasswordPolicy enforces the minimum password requirements.
func ValidatePasswordPolicy(username, password string) error {
	if len(password) < MinPasswordLen {
		return domain.ErrWeakPassword
	}
	if strings.TrimSpace(password) == "" {
		return domain.ErrWeakPassword
	}
	if strings.EqualFold(strings.TrimSpace(password), strings.TrimSpace(username)) {
		return domain.ErrWeakPassword
	}
	return nil
}

type hashParams struct {
	iterations int
	salt       []byte
	key        []byte
}

func parsePasswordHash(encoded string) (hashParams, bool) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 {
		return hashParams{}, false
	}
	if parts[0] != hashPrefix || parts[1] != hashFormat {
		return hashParams{}, false
	}

	rawIterations := strings.TrimPrefix(parts[2], "i=")
	if rawIterations == parts[2] {
		return hashParams{}, false
	}
	iterations, err := strconv.Atoi(rawIterations)
	if err != nil || iterations < MinIterations {
		return hashParams{}, false
	}

	salt, err := hashEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < saltLength {
		return hashParams{}, false
	}

	key, err := hashEncoding.DecodeString(parts[4])
	if err != nil || len(key) != keyLength {
		return hashParams{}, false
	}

	return hashParams{iterations: iterations, salt: salt, key: key}, true
}
