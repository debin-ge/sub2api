package jsonstrict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRejectDuplicateKeys(t *testing.T) {
	for _, valid := range []string{`null`, `{"a":1,"nested":{"a":2},"items":[{"b":1},{"b":2}]}`} {
		require.NoError(t, RejectDuplicateKeys([]byte(valid)), valid)
	}
	for _, invalid := range []string{
		`{"model":"first","model":"second"}`,
		`{"nested":{"id":"one","id":"two"}}`,
		`[{"id":1,"id":2}]`,
		`{"a":1}{"b":2}`,
	} {
		require.Error(t, RejectDuplicateKeys([]byte(invalid)), invalid)
	}
}
