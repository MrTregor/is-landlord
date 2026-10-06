package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testKey = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=" // 32 байта

func setBase(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://landlord_app:x@localhost:5432/landlord")
	t.Setenv("PDN_ENC_KEY", testKey)
}

func TestLoadDefaults(t *testing.T) {
	setBase(t)
	c, err := Load()
	require.NoError(t, err)
	assert.Equal(t, EnvDev, c.AppEnv)
	assert.Equal(t, "Europe/Moscow", c.Location.String())
	assert.Len(t, c.PDNKey, 32)
	assert.Equal(t, c.DatabaseURL, c.MigrateURL())
	assert.False(t, c.IsDemo())
}

func TestLoadRejectsBadValues(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("PDN_ENC_KEY", "короткий")
	t.Setenv("SMTP_TLS", "ssl")
	_, err := Load()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APP_ENV")
	assert.Contains(t, err.Error(), "PDN_ENC_KEY")
	assert.Contains(t, err.Error(), "SMTP_TLS")
}

func TestDemoClockOnlyInDemo(t *testing.T) {
	setBase(t)
	t.Setenv("DEMO_CLOCK_ENABLED", "true")
	_, err := Load()
	require.Error(t, err)

	t.Setenv("APP_ENV", "demo")
	c, err := Load()
	require.NoError(t, err)
	assert.True(t, c.IsDemo())
}

func TestProdRequiresSecureCookie(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "prod")
	t.Setenv("COOKIE_SECURE", "false")
	_, err := Load()
	require.Error(t, err)
}
