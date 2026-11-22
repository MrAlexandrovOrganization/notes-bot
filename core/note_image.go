package core

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"notes-bot/internal/noteimage"
	pb "notes-bot/proto/notes"
)

var imageSlots = make(chan struct{}, 2)

func visibleVaultPath(p string) bool {
	if !fs.ValidPath(p) || strings.ContainsAny(p, ":\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	return true
}

func regularVaultFile(root *os.Root, name string) (*os.File, error) {
	if !visibleVaultPath(name) {
		return nil, fs.ErrNotExist
	}
	for dir := name; dir != "."; dir = path.Dir(dir) {
		info, err := root.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (dir == name && !info.Mode().IsRegular()) {
			return nil, fs.ErrNotExist
		}
	}
	return root.Open(name)
}

func findImage(ctx context.Context, root *os.Root, note, ref string) (*os.File, error) {
	for _, p := range []string{path.Join(path.Dir(note), ref), path.Clean(ref)} {
		if f, err := regularVaultFile(root, p); err == nil {
			return f, nil
		}
	}
	if path.Base(ref) != ref {
		return nil, fs.ErrNotExist
	}
	match := ""
	err := fs.WalkDir(root.FS(), ".", func(p string, e fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return err
		}
		if p != "." && strings.HasPrefix(e.Name(), ".") && e.IsDir() {
			return fs.SkipDir
		}
		if e.Type().IsRegular() && e.Name() == ref {
			if match != "" {
				return errors.New("ambiguous image")
			}
			match = p
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return regularVaultFile(root, match)
}

func (s *NotesServer) ReadNoteImage(ctx context.Context, req *pb.ReadNoteImageRequest) (*pb.ReadNoteImageResponse, error) {
	select {
	case imageSlots <- struct{}{}:
		defer func() { <-imageSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	root, err := os.OpenRoot(GetConfig(ctx).NotesDir)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "vault unavailable")
	}
	defer root.Close()
	return readNoteImage(ctx, root, req.Note, req.Reference)
}

func readNoteImage(ctx context.Context, root *os.Root, note, ref string) (*pb.ReadNoteImageResponse, error) {
	missing := status.Error(codes.NotFound, "embedded image unavailable")
	if path.Ext(note) != ".md" || ref == "" {
		return nil, missing
	}
	n, err := regularVaultFile(root, note)
	if err != nil {
		return nil, missing
	}
	defer n.Close()
	body, err := io.ReadAll(io.LimitReader(n, markdownLimit+1))
	if err != nil || len(body) > markdownLimit {
		return nil, missing
	}
	linked := false
	for _, e := range noteimage.Parse(string(body)) {
		if e.Reference == ref {
			linked = true
			break
		}
	}
	if !linked {
		return nil, missing
	}
	f, err := findImage(ctx, root, note, ref)
	if err != nil {
		return nil, missing
	}
	defer f.Close()
	const maxBytes = 24 << 20
	stat, err := f.Stat()
	if err != nil || stat.Size() > maxBytes {
		return nil, missing
	}
	config, _, err := image.DecodeConfig(io.LimitReader(f, maxBytes))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil, missing
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return nil, missing
	}
	img, _, err := image.Decode(io.LimitReader(f, maxBytes))
	if err != nil {
		return nil, missing
	}
	// A bounded preview fits Telegram photo uploads and the default gRPC receive limit.
	size := max(config.Width, config.Height, 1600)
	dst := image.NewRGBA(image.Rect(0, 0, max(1, config.Width*1600/size), max(1, config.Height*1600/size)))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
		return nil, missing
	}
	return &pb.ReadNoteImageResponse{Data: buf.Bytes(), MediaType: "image/jpeg"}, nil
}
