package crypto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Быстрые параметры для тестов; формат и проверка те же.
var fast = Argon2Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestPasswordHash_NFR_Sec_01(t *testing.T) {
	h, err := HashPassword("Пароль-2026-x", DefaultArgon2)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$"), h)

	ok, rehash, err := VerifyPassword("Пароль-2026-x", h, DefaultArgon2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.False(t, rehash)

	ok, _, err = VerifyPassword("неверный", h, DefaultArgon2)
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestPasswordNeedsRehash(t *testing.T) {
	h, err := HashPassword("secret-123456", fast)
	require.NoError(t, err)
	ok, rehash, err := VerifyPassword("secret-123456", h, DefaultArgon2)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.True(t, rehash)
}

func TestPasswordBadHash(t *testing.T) {
	_, _, err := VerifyPassword("x", "$2a$10$abc", DefaultArgon2)
	assert.ErrorIs(t, err, ErrBadHash)
}

func TestTokens(t *testing.T) {
	a, b := NewToken(), NewToken()
	assert.NotEqual(t, a, b)
	assert.Len(t, a, 43) // 32 байта в base64url без выравнивания
	assert.Len(t, HashToken(a), 32)
	assert.Equal(t, HashToken(a), HashToken(a))
	assert.True(t, EqualConstantTime(a, a))
	assert.False(t, EqualConstantTime(a, b))
}
