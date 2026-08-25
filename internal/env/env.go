// Package env provides typed environment variable readers with defaults,
// shared by every service's config loader so parsing rules stay consistent.
package env

import (
	"os"
	"strconv"
)

// Str returns the environment value for key, or def when unset/empty.
func Str(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Int returns the environment value parsed as int, or def when unset/empty
// or not a valid integer.
func Int(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Bool returns the environment value parsed as bool (1/t/T/true/TRUE accepted
// by strconv.ParseBool), or def when unset/empty/unparsable.
func Bool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

// Required returns the environment value or an error when unset/empty.
func Required(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", &MissingError{Key: key}
	}
	return v, nil
}

// MissingError reports a required environment variable that was not set.
type MissingError struct{ Key string }

func (e *MissingError) Error() string { return e.Key + " is not set" }
