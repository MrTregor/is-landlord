package crypto

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// TokenBytes — длина случайных токенов (сессия, CSRF, приглашение, восстановление), байт.
const TokenBytes = 32

// NewToken возвращает 32 случайных байта в base64url без выравнивания.
func NewToken() string {
	b := make([]byte, TokenBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand не возвращает ошибок на поддерживаемых системах
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken — sha256 значения токена; в БД хранится только он.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// EqualConstantTime сравнивает строки за постоянное время (CSRF-токен).
func EqualConstantTime(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
