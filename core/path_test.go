package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveVaultPath(t *testing.T) {
	vault := t.TempDir()
	absVault, _ := filepath.Abs(vault)

	cases := []struct {
		name    string
		relpath string
		wantErr bool
		want    string
	}{
		{name: "ok daily", relpath: "Daily/01-Jan-2025.md", want: filepath.Join(absVault, "Daily/01-Jan-2025.md")},
		{name: "ok nested", relpath: "Projects/sub/note.md", want: filepath.Join(absVault, "Projects/sub/note.md")},
		{name: "reject parent", relpath: "../escape.md", wantErr: true},
		{name: "reject deep parent", relpath: "Daily/../../escape.md", wantErr: true},
		{name: "reject absolute", relpath: "/etc/passwd", wantErr: true},
		{name: "reject empty", relpath: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveVaultPath(vault, tc.relpath)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveVaultPathRejectsEscapingSymlink(t *testing.T) {
	vault := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.md")
	if err := os.WriteFile(target, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(vault, "escape.md")); err != nil {
		t.Fatal(err)
	}

	if got, err := resolveVaultPath(vault, "escape.md"); err == nil {
		t.Fatalf("want escaping symlink error, got %q", got)
	}
}

func TestValidateDate(t *testing.T) {
	for _, date := range []string{"../Templates/Daily", "01-Mar-2026/../../escape", "/etc/passwd", "2026-03-01", "1-Mar-2026"} {
		t.Run(date, func(t *testing.T) {
			if err := validateDate(date); err == nil {
				t.Fatalf("want invalid date error for %q", date)
			}
		})
	}
	if err := validateDate("01-Mar-2026"); err != nil {
		t.Fatalf("valid date rejected: %v", err)
	}
}
