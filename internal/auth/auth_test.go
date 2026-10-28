package auth_test

import (
	"strings"
	"testing"

	"github.com/BreakDown-CS/StockFlowLite/internal/auth"
	"github.com/BreakDown-CS/StockFlowLite/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testHasher() *auth.Hasher {
	return auth.NewHasher(auth.MinIterations)
}

func TestHasher_HashAndVerify(t *testing.T) {
	hasher := testHasher()

	hash, err := hasher.Hash("correct-horse-battery")
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(hash, "pbkdf2-sha256$v=1$i="), "hash must be versioned")
	assert.NotContains(t, hash, "correct-horse-battery", "hash must never contain the password")
	assert.LessOrEqual(t, len(hash), 255, "hash must fit the users.password_hash column")

	assert.True(t, hasher.Verify(hash, "correct-horse-battery"))
	assert.False(t, hasher.Verify(hash, "wrong-password"))
	assert.False(t, hasher.Verify(hash, ""))
}

func TestHasher_UniqueSaltPerHash(t *testing.T) {
	hasher := testHasher()

	first, err := hasher.Hash("same-password")
	require.NoError(t, err)
	second, err := hasher.Hash("same-password")
	require.NoError(t, err)

	assert.NotEqual(t, first, second, "each hash must use a fresh salt")
	assert.True(t, hasher.Verify(first, "same-password"))
	assert.True(t, hasher.Verify(second, "same-password"))
}

func TestHasher_RejectsUnknownOrTamperedHashes(t *testing.T) {
	hasher := testHasher()

	invalid := []string{
		"",
		"!disabled-development-identity", // Phase 5 seeded placeholder
		"plaintext",
		"pbkdf2-sha256$v=1$i=100000$onlythree$parts",
		"pbkdf2-sha256$v=2$i=100000$c2FsdA$a2V5", // unsupported version
		"bcrypt$v=1$i=100000$c2FsdA$a2V5",        // foreign algorithm
		"pbkdf2-sha256$v=1$i=1$c2FsdA$a2V5",      // below the minimum work factor
	}

	for _, hash := range invalid {
		assert.False(t, hasher.Verify(hash, "anything"), "hash %q must never verify", hash)
	}
}

func TestHasher_NeedsRehash(t *testing.T) {
	weak := auth.NewHasher(auth.MinIterations)
	strong := auth.NewHasher(auth.DefaultIterations)

	weakHash, err := weak.Hash("password123")
	require.NoError(t, err)

	assert.False(t, weak.NeedsRehash(weakHash))
	assert.True(t, strong.NeedsRehash(weakHash))
	assert.True(t, strong.NeedsRehash("!disabled-development-identity"))
}

func TestHasher_DefaultIterationsFloor(t *testing.T) {
	hasher := auth.NewHasher(1)
	assert.Equal(t, auth.DefaultIterations, hasher.Iterations(), "unsafe iteration counts must fall back to the default")
}

func TestHasher_VerifyRejectsOverlongPassword(t *testing.T) {
	hasher := testHasher()

	hash, err := hasher.Hash("short-enough")
	require.NoError(t, err)

	assert.False(t, hasher.Verify(hash, strings.Repeat("a", auth.MaxPasswordLen+1)))
}

func TestValidatePasswordPolicy(t *testing.T) {
	assert.NoError(t, auth.ValidatePasswordPolicy("alice", "s3cret-pass"))
	assert.ErrorIs(t, auth.ValidatePasswordPolicy("alice", "short"), domain.ErrWeakPassword)
	assert.ErrorIs(t, auth.ValidatePasswordPolicy("alice", ""), domain.ErrWeakPassword)
	assert.ErrorIs(t, auth.ValidatePasswordPolicy("Alice", "alice"), domain.ErrWeakPassword)
	assert.ErrorIs(t, auth.ValidatePasswordPolicy("alice", strings.Repeat("a", auth.MaxPasswordLen+1)), domain.ErrPasswordTooLong)
}

func TestGenerateToken(t *testing.T) {
	const pepper = "unit-test-pepper"

	token, tokenHash, err := auth.GenerateToken(pepper)
	require.NoError(t, err)

	assert.NotEmpty(t, token)
	assert.GreaterOrEqual(t, len(token), 40, "token must carry enough entropy")
	assert.Len(t, tokenHash, 64, "stored hash is hex-encoded HMAC-SHA256")
	assert.Equal(t, tokenHash, auth.HashToken(pepper, token))
	assert.NotContains(t, tokenHash, token, "stored hash must not equal the raw token")

	otherToken, otherHash, err := auth.GenerateToken(pepper)
	require.NoError(t, err)
	assert.NotEqual(t, token, otherToken)
	assert.NotEqual(t, tokenHash, otherHash)
	assert.NotEqual(t, auth.HashToken("different-pepper", token), tokenHash)
}
