// Package crypto — хеширование паролей (argon2id), случайные токены, шифрование ПДн.
package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Параметры argon2id (12 Безопасность и ПДн, раздел 1): память 64 МиБ, 3 итерации, 2 потока,
// соль 16 байт, ключ 32 байта.
type Argon2Params struct {
	MemoryKiB uint32
	Time      uint32
	Threads   uint8
	SaltLen   uint32
	KeyLen    uint32
}

// DefaultArgon2 — параметры по спецификации.
var DefaultArgon2 = Argon2Params{MemoryKiB: 64 * 1024, Time: 3, Threads: 2, SaltLen: 16, KeyLen: 32}

// ErrBadHash — строка хеша не в формате PHC argon2id.
var ErrBadHash = errors.New("неверный формат хеша пароля")

var b64 = base64.RawStdEncoding

// HashPassword возвращает хеш в формате PHC: $argon2id$v=19$m=65536,t=3,p=2$<соль>$<хеш>.
func HashPassword(password string, p Argon2Params) (string, error) {
	salt := make([]byte, p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, p.Time, p.MemoryKiB, p.Threads, p.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.Time, p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

type parsedHash struct {
	params Argon2Params
	salt   []byte
	key    []byte
}

func parseHash(encoded string) (*parsedHash, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, ErrBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, ErrBadHash
	}
	var p Argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.MemoryKiB, &p.Time, &p.Threads); err != nil {
		return nil, ErrBadHash
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil {
		return nil, ErrBadHash
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil {
		return nil, ErrBadHash
	}
	p.SaltLen = uint32(len(salt)) //nolint:gosec // длина соли мала
	p.KeyLen = uint32(len(key))   //nolint:gosec // длина ключа мала
	return &parsedHash{params: p, salt: salt, key: key}, nil
}

// VerifyPassword сравнивает пароль с хешем за постоянное время.
// needsRehash = true, если хеш построен с устаревшими параметрами (его нужно пересчитать при входе).
func VerifyPassword(password, encoded string, current Argon2Params) (ok, needsRehash bool, err error) {
	h, err := parseHash(encoded)
	if err != nil {
		return false, false, err
	}
	key := argon2.IDKey([]byte(password), h.salt, h.params.Time, h.params.MemoryKiB, h.params.Threads, h.params.KeyLen)
	ok = subtle.ConstantTimeCompare(key, h.key) == 1
	return ok, ok && h.params != current, nil
}
