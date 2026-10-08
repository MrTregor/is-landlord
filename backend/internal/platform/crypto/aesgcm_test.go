package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testKey = bytes.Repeat([]byte{7}, 32)

// Шифрование паспортных данных и адреса регистрации (NFR-Sec-03, 12 Безопасность и ПДн, раздел 5).
func TestEncryptDecrypt_NFR_Sec_03(t *testing.T) {
	c, err := NewCipher(testKey)
	require.NoError(t, err)
	aad := TenantAAD("0199f1b2-7c3e-7a10-9d4e-3f5a6b7c8d90", "doc_number")

	enc, err := c.Encrypt("45 18 123456", aad)
	require.NoError(t, err)
	assert.Len(t, enc, 12+len("45 18 123456")+16, "nonce(12) || ciphertext || tag(16)")
	assert.NotContains(t, string(enc), "123456")

	plain, err := c.Decrypt(enc, aad)
	require.NoError(t, err)
	assert.Equal(t, "45 18 123456", plain)

	// Одинаковые значения шифруются по-разному (случайный nonce).
	enc2, _ := c.Encrypt("45 18 123456", aad)
	assert.NotEqual(t, enc, enc2)
}

func TestDecryptRejectsWrongAADOrKey(t *testing.T) {
	c, _ := NewCipher(testKey)
	enc, _ := c.Encrypt("г. Москва, ул. Примерная, д. 1", TenantAAD("a", "registration_address"))

	_, err := c.Decrypt(enc, TenantAAD("b", "registration_address"))
	assert.ErrorIs(t, err, ErrDecrypt, "значение другого нанимателя не подставить")
	_, err = c.Decrypt(enc, TenantAAD("a", "doc_number"))
	assert.ErrorIs(t, err, ErrDecrypt, "значение другого поля не подставить")

	other, _ := NewCipher(bytes.Repeat([]byte{8}, 32))
	_, err = other.Decrypt(enc, TenantAAD("a", "registration_address"))
	assert.ErrorIs(t, err, ErrDecrypt)

	enc[len(enc)-1] ^= 1
	_, err = c.Decrypt(enc, TenantAAD("a", "registration_address"))
	assert.ErrorIs(t, err, ErrDecrypt, "повреждённое значение")
}

func TestEmptyValues(t *testing.T) {
	c, _ := NewCipher(testKey)
	enc, err := c.Encrypt("", "x")
	require.NoError(t, err)
	assert.Nil(t, enc)
	plain, err := c.Decrypt(nil, "x")
	require.NoError(t, err)
	assert.Empty(t, plain)

	_, err = NewCipher([]byte("короткий"))
	assert.Error(t, err)
}
