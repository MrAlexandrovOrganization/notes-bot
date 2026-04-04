package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	frontmatterWithRating    = "---\ndate: \"[[01-Mar-2026]]\"\nОценка: 7\n---\ncontent\n"
	frontmatterWithoutRating = "---\ndate: \"[[01-Mar-2026]]\"\ntags:\n  - daily\n---\ncontent\n"
	frontmatterEmptyRating   = "---\nОценка:\n---\ncontent\n"
	invalidFrontmatter       = "no delimiters here"
)

// --- GetRatingImpl ---

func TestGetRatingImpl_ReturnsValue(t *testing.T) {
	rating := GetRatingImpl(t.Context(), frontmatterWithRating)
	require.NotNil(t, rating)
	assert.Equal(t, 7, *rating)
}

func TestGetRatingImpl_NoRatingField(t *testing.T) {
	assert.Nil(t, GetRatingImpl(t.Context(), frontmatterWithoutRating))
}

func TestGetRatingImpl_EmptyRatingValue(t *testing.T) {
	assert.Nil(t, GetRatingImpl(t.Context(), frontmatterEmptyRating))
}

func TestGetRatingImpl_InvalidFrontmatter(t *testing.T) {
	assert.Nil(t, GetRatingImpl(t.Context(), invalidFrontmatter))
}

func TestGetRatingImpl_Zero(t *testing.T) {
	rating := GetRatingImpl(t.Context(), "---\nОценка: 0\n---\ncontent\n")
	require.NotNil(t, rating)
	assert.Equal(t, 0, *rating)
}

func TestGetRatingImpl_Ten(t *testing.T) {
	rating := GetRatingImpl(t.Context(), "---\nОценка: 10\n---\ncontent\n")
	require.NotNil(t, rating)
	assert.Equal(t, 10, *rating)
}

// --- UpdateRatingImpl ---

func TestUpdateRatingImpl_UpdatesExistingField(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), frontmatterWithRating, 3)
	require.True(t, ok)
	assert.Contains(t, result, "Оценка: 3")
	assert.NotContains(t, result, "Оценка: 7")
}

func TestUpdateRatingImpl_AddsFieldWhenMissing(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), frontmatterWithoutRating, 5)
	require.True(t, ok)
	assert.Contains(t, result, "Оценка: 5")
}

func TestUpdateRatingImpl_Roundtrip(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), frontmatterWithRating, 9)
	require.True(t, ok)
	rating := GetRatingImpl(t.Context(), result)
	require.NotNil(t, rating)
	assert.Equal(t, 9, *rating)
}

func TestUpdateRatingImpl_InvalidFrontmatter(t *testing.T) {
	_, ok := UpdateRatingImpl(t.Context(), invalidFrontmatter, 5)
	assert.False(t, ok)
}

func TestUpdateRatingImpl_PreservesOtherFields(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), frontmatterWithRating, 2)
	require.True(t, ok)
	assert.Contains(t, result, "date: \"[[01-Mar-2026]]\"")
	assert.Contains(t, result, "content")
}
