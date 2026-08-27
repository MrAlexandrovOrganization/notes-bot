// Package duration parses human-readable duration strings used for reminder
// postponement and renders minute counts back as Russian labels. It is shared
// by the Telegram and web frontends so parsing rules never drift apart.
package duration

import (
	"fmt"
	"maps"
	"strconv"
	"strings"
)

// MinutesToLabel converts a minute count to a human-readable Russian label.
// Mixed durations are rendered as "1 д. 3 ч.", "2 ч. 30 мин.", etc.
func MinutesToLabel(n int) string {
	const month = 30 * 24 * 60
	const week = 7 * 24 * 60
	const day = 24 * 60
	const hour = 60

	var parts []string
	if n >= month {
		parts = append(parts, fmt.Sprintf("%d мес.", n/month))
		n %= month
	}
	if n >= week {
		parts = append(parts, fmt.Sprintf("%d нед.", n/week))
		n %= week
	}
	if n >= day {
		parts = append(parts, fmt.Sprintf("%d д.", n/day))
		n %= day
	}
	if n >= hour {
		parts = append(parts, fmt.Sprintf("%d ч.", n/hour))
		n %= hour
	}
	if n > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d мин.", n))
	}
	return strings.Join(parts, " ")
}

// Parse parses a human-readable duration string into total minutes.
//
// Supported units (case-sensitive):
//
//	m — минуты   h — часы   d — дни   w — недели   M — месяцы (≈ 30 дней)
//
// Formats:
//
//	30m  2h30m  1d12h  1w  1M  3d6h30m  (spaces between tokens are OK)
//
// A bare integer is accepted as minutes for backward compatibility.
//
// Returns an informative error with a suggested canonical form when a unit
// value overflows into the next unit (e.g. 27h → error suggesting "1d3h").
func Parse(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Bare integer → minutes (backward compat)
	if n, err := strconv.Atoi(s); err == nil {
		if n <= 0 {
			return 0, fmt.Errorf("введите положительное значение")
		}
		return n, nil
	}

	// Remove spaces so "1d 3h 30m" → "1d3h30m"
	s = strings.ReplaceAll(s, " ", "")

	vals := make(map[byte]int)
	i := 0
	for i < len(s) {
		// Read run of digits
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i {
			return 0, fmt.Errorf("неверный формат — ожидается число перед единицей. Примеры: 30m, 2h30m, 1d12h")
		}
		if j >= len(s) {
			return 0, fmt.Errorf("неверный формат — укажите единицу после числа. Доступные: m h d w M")
		}
		n, _ := strconv.Atoi(s[i:j])
		unit := s[j]
		switch unit {
		case 'm', 'h', 'd', 'w', 'M':
		default:
			return 0, fmt.Errorf("неизвестная единица %q. Доступные: m (минуты), h (часы), d (дни), w (недели), M (месяцы)", string(unit))
		}
		if _, exists := vals[unit]; exists {
			return 0, fmt.Errorf("единица %q указана дважды", string(unit))
		}
		vals[unit] = n
		i = j + 1
	}

	if len(vals) == 0 {
		return 0, fmt.Errorf("неверный формат. Примеры: 30m, 2h30m, 1d12h, 1w, 1M")
	}

	// Validate: each unit must be within its canonical range.
	if m, ok := vals['m']; ok && m >= 60 {
		sugg := suggestion(vals, 'm')
		return 0, fmt.Errorf("%dm — это %s; введите: %s", m, overflowDesc(m, 'm'), sugg)
	}
	if h, ok := vals['h']; ok && h >= 24 {
		sugg := suggestion(vals, 'h')
		return 0, fmt.Errorf("%dh — это %s; введите: %s", h, overflowDesc(h, 'h'), sugg)
	}
	if d, ok := vals['d']; ok && d >= 7 {
		sugg := suggestion(vals, 'd')
		return 0, fmt.Errorf("%dd — это %s; введите: %s", d, overflowDesc(d, 'd'), sugg)
	}

	total := vals['m'] + vals['h']*60 + vals['d']*1440 + vals['w']*10080 + vals['M']*43200
	if total <= 0 {
		return 0, fmt.Errorf("введите положительное значение")
	}
	return total, nil
}

// overflowDesc returns a human-readable description of what an overflowing
// unit value actually equals. E.g. 27 hours → "1д 3ч".
func overflowDesc(val int, unit byte) string {
	switch unit {
	case 'm':
		h, m := val/60, val%60
		if m > 0 {
			return fmt.Sprintf("%dч %dм", h, m)
		}
		return fmt.Sprintf("%dч", h)
	case 'h':
		d, h := val/24, val%24
		if h > 0 {
			return fmt.Sprintf("%dд %dч", d, h)
		}
		return fmt.Sprintf("%dд", d)
	case 'd':
		w, d := val/7, val%7
		if d > 0 {
			return fmt.Sprintf("%dн %dд", w, d)
		}
		return fmt.Sprintf("%dн", w)
	}
	return fmt.Sprintf("%d", val)
}

// suggestion builds a canonical duration string by normalising the
// overflowing unit and carrying into higher units.
func suggestion(vals map[byte]int, overflowUnit byte) string {
	nv := make(map[byte]int, len(vals))
	maps.Copy(nv, vals)

	switch overflowUnit {
	case 'm':
		nv['h'] += nv['m'] / 60
		nv['m'] = nv['m'] % 60
		fallthrough // h might now overflow too
	case 'h':
		if overflowUnit == 'h' || nv['h'] >= 24 {
			nv['d'] += nv['h'] / 24
			nv['h'] = nv['h'] % 24
		}
		fallthrough
	case 'd':
		if overflowUnit == 'd' || nv['d'] >= 7 {
			nv['w'] += nv['d'] / 7
			nv['d'] = nv['d'] % 7
		}
	}

	var parts []string
	for _, u := range []byte{'M', 'w', 'd', 'h', 'm'} {
		if v := nv[u]; v > 0 {
			parts = append(parts, fmt.Sprintf("%d%c", v, u))
		}
	}
	if len(parts) == 0 {
		return "0m"
	}
	return strings.Join(parts, "")
}
