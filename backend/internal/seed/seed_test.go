package seed

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"landlord/backend/internal/config"
)

// В рабочем окружении seed --demo отказывается работать до любых действий с базой (№ 103).
func TestRunRefusesInProd_NFR_Sec_05(t *testing.T) {
	cfg := &config.Config{AppEnv: config.EnvProd, DatabaseURL: "postgres://нет-такой-базы"}
	err := Run(context.Background(), cfg, slog.New(slog.DiscardHandler), io.Discard, Options{})
	require.ErrorIs(t, err, ErrProd)
	require.EqualError(t, err, "Загрузка демо-данных запрещена в рабочем окружении")
}
