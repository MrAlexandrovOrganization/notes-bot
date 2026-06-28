package core

import (
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
