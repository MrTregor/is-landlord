package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Cipher — шифрование полей с ПДн (12 Безопасность и ПДн, раздел 5): AES-256-GCM,
// значение = nonce(12) || ciphertext || tag, дополнительные данные (AAD) = «tenant:<id>:<поле>».
type Cipher struct {
	aead cipher.AEAD
}

// ErrDecrypt — значение повреждено, ключ неверен или AAD не совпадает.
var ErrDecrypt = errors.New("не удалось расшифровать значение")

// NewCipher создаёт шифр по ключу PDN_ENC_KEY (32 байта).
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("ключ шифрования: ожидается 32 байта, получено %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt шифрует строку; пустая строка → nil (поле не заполнено).
func (c *Cipher) Encrypt(plaintext, aad string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, []byte(plaintext), []byte(aad)), nil
}

// Decrypt расшифровывает значение; nil → пустая строка.
func (c *Cipher) Decrypt(value []byte, aad string) (string, error) {
	if len(value) == 0 {
		return "", nil
	}
	n := c.aead.NonceSize()
	if len(value) < n+c.aead.Overhead() {
		return "", ErrDecrypt
	}
	plain, err := c.aead.Open(nil, value[:n], value[n:], []byte(aad))
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}

// TenantAAD — дополнительные данные для поля нанимателя.
func TenantAAD(tenantID, field string) string { return "tenant:" + tenantID + ":" + field }
