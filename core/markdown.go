package core

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "notes-bot/proto/notes"
)

// Keep the content and its revision within the default gRPC message limit.
const markdownLimit = 3 << 20

const missingMarkdownRevision = "missing"

func markdownPath(ctx context.Context, relative string) (string, error) {
	if filepath.Ext(relative) != ".md" || filepath.Clean(relative) != relative {
		return "", status.Error(codes.InvalidArgument, "expected a canonical Markdown path")
	}
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return "", status.Error(codes.InvalidArgument, "hidden Markdown paths are unsupported")
		}
	}
	root := GetConfig(ctx).NotesDir
	path, err := resolveVaultPath(root, relative)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "invalid Markdown path")
	}
	vault, err := filepath.Abs(root)
	if err != nil {
		return "", status.Error(codes.Internal, "unable to resolve Markdown directory")
	}
	for dir := filepath.Dir(path); dir != vault; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() {
			return "", status.Error(codes.FailedPrecondition, "expected regular Markdown directories")
		}
	}
	return path, nil
}

func readMarkdown(path string) (*pb.ReadMarkdownResponse, os.FileMode, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return &pb.ReadMarkdownResponse{Revision: missingMarkdownRevision}, 0644, nil
	}
	if err != nil {
		return nil, 0, status.Error(codes.Internal, "unable to check Markdown file")
	}
	if !info.Mode().IsRegular() {
		return nil, 0, status.Error(codes.FailedPrecondition, "expected a regular Markdown file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, status.Error(codes.Internal, "unable to open Markdown file")
	}
	defer f.Close()
	content, err := io.ReadAll(io.LimitReader(f, markdownLimit+1))
	if err != nil {
		return nil, 0, status.Error(codes.Internal, "unable to read Markdown file")
	}
	if len(content) > markdownLimit || !utf8.Valid(content) {
		return nil, 0, status.Error(codes.FailedPrecondition, "unsupported Markdown content")
	}
	return &pb.ReadMarkdownResponse{
		Content: string(content), Revision: fmt.Sprintf("%x", sha256.Sum256(content)),
	}, info.Mode().Perm(), nil
}

func (s *NotesServer) ReadMarkdown(ctx context.Context, req *pb.ReadMarkdownRequest) (*pb.ReadMarkdownResponse, error) {
	path, err := markdownPath(ctx, req.GetRelpath())
	if err != nil {
		return nil, err
	}
	snapshot, _, err := readMarkdown(path)
	return snapshot, err
}

func (s *NotesServer) ReplaceMarkdown(ctx context.Context, req *pb.ReplaceMarkdownRequest) (*pb.ReplaceMarkdownResponse, error) {
	if req.GetExpectedRevision() == "" || len(req.GetContent()) > markdownLimit || !utf8.ValidString(req.GetContent()) {
		return nil, status.Error(codes.InvalidArgument, "invalid Markdown replacement")
	}
	path, err := markdownPath(ctx, req.GetRelpath())
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, status.Error(codes.Internal, "unable to prepare Markdown directory")
	}
	unlock, err := lockFile(ctx, path)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Internal, "unable to lock Markdown file")
	}
	defer unlock()
	current, mode, err := readMarkdown(path)
	if err != nil {
		return nil, err
	}
	if current.GetRevision() != req.GetExpectedRevision() {
		return nil, status.Error(codes.Aborted, "Markdown changed")
	}
	if current.Revision != missingMarkdownRevision && current.Content == req.GetContent() {
		if err := syncMarkdown(path); err != nil {
			return nil, status.Error(codes.Internal, "unable to sync Markdown file")
		}
	} else if err := writeFileAtomic(path, []byte(req.GetContent()), mode); err != nil {
		return nil, status.Error(codes.Internal, "unable to save Markdown file")
	}
	// The first write can also create directories in the vault.
	vault, err := filepath.Abs(GetConfig(ctx).NotesDir)
	if err != nil {
		return nil, status.Error(codes.Internal, "unable to resolve Markdown directory")
	}
	for dir != vault {
		dir = filepath.Dir(dir)
		if err := syncDirectory(dir); err != nil {
			return nil, status.Error(codes.Internal, "unable to sync Markdown directory")
		}
	}
	return &pb.ReplaceMarkdownResponse{}, nil
}

func syncMarkdown(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
