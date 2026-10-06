package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// Встроенная спецификация API корректна (NFR-Comp-02).
func TestOpenAPISpecValid_NFR_Comp_02(t *testing.T) {
	spec, err := GetSwagger()
	require.NoError(t, err)
	require.NoError(t, spec.Validate(context.Background()))
	require.Equal(t, "3.1.0", spec.OpenAPI)
}
