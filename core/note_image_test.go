package core

import (
	"context"
	"image"
	"image/png"
	pb "notes-bot/proto/notes"
	"os"
	"path/filepath"
	"testing"
)

func TestOnlyLinkedVaultImages(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "Media"), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, "Media", "a b.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, image.NewRGBA(image.Rect(0, 0, 5, 3))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = os.WriteFile(filepath.Join(dir, "note.md"), []byte("![[a b.png|360]] ![sample](Media/a%20b.png) ![[escape.png]]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dir, "Media", "a b.png"), filepath.Join(dir, "escape.png")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	for _, ref := range []string{"a b.png", "Media/a b.png", "escape.png", "other.png", "../secret.png"} {
		var img *pb.ReadNoteImageResponse
		img, err = readNoteImage(context.Background(), root, "note.md", ref)
		want := ref == "a b.png" || ref == "Media/a b.png"
		if (err == nil) != want || (want && len(img.Data) == 0) {
			t.Fatalf("unexpected result for %q: %v", ref, err)
		}
	}
}
