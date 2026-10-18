package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"landlord/backend/internal/jobrun"
)

// Каждая задача экрана «Служебное» запускается вручную: у всех задач каталога 07 есть описание запуска
// (заглушек «появится на этапе …» больше нет, FR-14.1).
func TestCatalogJobsRunnable_FR14_1(t *testing.T) {
	known := map[string]bool{}
	for _, d := range append(Daily(), Periodic()...) {
		known[d.Name] = true
	}
	for _, j := range jobrun.Catalog {
		assert.True(t, known[j.Name], j.Name)
	}
}
