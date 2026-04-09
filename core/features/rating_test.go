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

// --- Obsidian format tests (3 delimiters) ---

const (
	obsidianFormatWithRating = `---
date: "[[09-Apr-2026]]"
title: "[[09-Apr-2026]]"
Оценка: 7
tags:
  - daily
---
- [x] Доброго утра!  [completion:: 2026-04-09]
- [x] Написать промпт для пагинации заметок  [completion:: 2026-04-09]
---
`

	obsidianFormatWithoutRating = `---
date: "[[09-Apr-2026]]"
title: "[[09-Apr-2026]]"
Оценка:
tags:
  - daily
---
- [x] Доброго утра!  [completion:: 2026-04-09]
- [x] Написать промпт для пагинации заметок  [completion:: 2026-04-09]
- [ ] Посчитать ресурсы
- [x] Узнать, каким образом обрабатывается трафик, если балансёры в 3 дц  [completion:: 2026-04-09]
---
`
)

func TestGetRatingImpl_ObsidianFormat_WithRating(t *testing.T) {
	rating := GetRatingImpl(t.Context(), obsidianFormatWithRating)
	require.NotNil(t, rating)
	assert.Equal(t, 7, *rating)
}

func TestGetRatingImpl_ObsidianFormat_EmptyRating(t *testing.T) {
	// Оценка: without value should return nil
	rating := GetRatingImpl(t.Context(), obsidianFormatWithoutRating)
	assert.Nil(t, rating)
}

func TestUpdateRatingImpl_ObsidianFormat_UpdatesExistingField(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), obsidianFormatWithRating, 3)
	require.True(t, ok)
	assert.Contains(t, result, "Оценка: 3")
	assert.NotContains(t, result, "Оценка: 7")
	// Should preserve content
	assert.Contains(t, result, "- [x] Доброго утра!")
}

func TestUpdateRatingImpl_ObsidianFormat_AddsFieldWhenMissing(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), obsidianFormatWithoutRating, 5)
	require.True(t, ok)
	assert.Contains(t, result, "Оценка: 5")
	// Should preserve content
	assert.Contains(t, result, "- [x] Доброго утра!")
}

func TestUpdateRatingImpl_ObsidianFormat_Roundtrip(t *testing.T) {
	result, ok := UpdateRatingImpl(t.Context(), obsidianFormatWithRating, 9)
	require.True(t, ok)
	rating := GetRatingImpl(t.Context(), result)
	require.NotNil(t, rating)
	assert.Equal(t, 9, *rating)
}
