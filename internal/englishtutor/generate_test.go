package englishtutor

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestWordMustHaveRussianMeaning(t *testing.T) {
	_, err := Prepare(t.Context(), "word", "gate", "", nil, func(context.Context, string, string, any, int) (string, error) {
		return `{"source":"gate","english":"gate","example":"The gate is open.","question":""}`, nil
	})
	require.Error(t, err)
}
