package searchquery

import (
	"testing"
	"time"
)

func TestExtractDateRange(t *testing.T) {
	today := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		query string
		from  string
		to    string
	}{
		{"что я делал вчера", "2026-07-31", "2026-07-31"},
		{"заметки позавчера", "2026-07-30", "2026-07-30"},
		{"что было на прошлой неделе", "2026-07-20", "2026-07-26"},
		{"итоги за последние 10 дней", "2026-07-23", "2026-08-01"},
		{"о чём писал в июле", "2026-07-01", "2026-07-31"},
		{"события в декабре", "2025-12-01", "2025-12-31"},
		{"между 01.07.2026 и 15.07.2026", "2026-07-01", "2026-07-15"},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := ExtractDateRange(tt.query, today)
			if got.From != tt.from || got.To != tt.to {
				t.Fatalf("got %#v, want %s..%s", got, tt.from, tt.to)
			}
		})
	}
}

func TestExtractDateRange_NoTemporalExpression(t *testing.T) {
	got := ExtractDateRange("где я писал про pgvector", time.Now())
	if !got.Empty() {
		t.Fatalf("unexpected range: %#v", got)
	}
}
