// LLM usage: generated with deepseek-v4-pro and manually modified.
package filesize_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/HT4w5/webjail/pkg/filesize"
	"github.com/rs/zerolog"
)

type testLocalConfig struct {
	routes map[string]string
}

func (c testLocalConfig) Routes() map[string]string {
	return c.routes
}

func TestLocal_Query_ExistingFile(t *testing.T) {
	dir := t.TempDir()

	content := []byte("hello, world")
	filename := "test.txt"
	if err := os.WriteFile(filepath.Join(dir, filename), content, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testLocalConfig{
		routes: map[string]string{
			"/": dir + "/",
		},
	}

	l := filesize.NewLocal(cfg, zerolog.Nop())
	ctx := context.Background()

	size, err := l.Query(ctx, "/"+filename)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != int64(len(content)) {
		t.Fatalf("expected size %d, got %d", len(content), size)
	}
}

func TestLocal_Query_NonExistentFile(t *testing.T) {
	dir := t.TempDir()

	cfg := testLocalConfig{
		routes: map[string]string{
			"/": dir + "/",
		},
	}

	l := filesize.NewLocal(cfg, zerolog.Nop())
	ctx := context.Background()

	_, err := l.Query(ctx, "/nope.txt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, filesize.ErrNoInfo) {
		t.Fatalf("expected ErrNoInfo, got %v", err)
	}
}

func TestLocal_Query_BadURL_Empty(t *testing.T) {
	l := filesize.NewLocal(testLocalConfig{}, zerolog.Nop())

	_, err := l.Query(context.Background(), "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, filesize.ErrBadURL) {
		t.Fatalf("expected ErrBadURL, got %v", err)
	}
}

func TestLocal_Query_BadURL_Relative(t *testing.T) {
	l := filesize.NewLocal(testLocalConfig{}, zerolog.Nop())

	_, err := l.Query(context.Background(), "relative/path")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, filesize.ErrBadURL) {
		t.Fatalf("expected ErrBadURL, got %v", err)
	}
}

func TestLocal_Query_Directory(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := testLocalConfig{
		routes: map[string]string{
			"/": dir + "/",
		},
	}

	l := filesize.NewLocal(cfg, zerolog.Nop())

	_, err := l.Query(context.Background(), "/sub")
	if err == nil {
		t.Fatal("expected error for directory, got nil")
	}
	if !errors.Is(err, filesize.ErrNoInfo) {
		t.Fatalf("expected ErrNoInfo, got %v", err)
	}
}

func TestLocal_Query_LongestPrefixMatch(t *testing.T) {
	// Two routes: "/" → dirA, "/data/" → dirB.
	// "/data/file.txt" should resolve to dirB, not dirA.
	dirA := t.TempDir()
	dirB := t.TempDir()

	contentA := []byte("from-dir-a")
	contentB := []byte("from-dir-b")

	// Create file in dirA: must be in an "other/" subdirectory
	// because the test queries "/other/file.txt" against the "/" route.
	if err := os.Mkdir(filepath.Join(dirA, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirA, "other", "file.txt"), contentA, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create file in dirB at the root.
	if err := os.WriteFile(filepath.Join(dirB, "file.txt"), contentB, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := testLocalConfig{
		routes: map[string]string{
			"/":      dirA + "/",
			"/data/": dirB + "/",
		},
	}

	l := filesize.NewLocal(cfg, zerolog.Nop())
	ctx := context.Background()

	// "/data/file.txt" → longest prefix "/data/" (2nd route) → dirB/file.txt.
	size, err := l.Query(ctx, "/data/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != int64(len(contentB)) {
		t.Fatalf("expected size %d (dirB), got %d (dirA?)", len(contentB), size)
	}

	// "/other/file.txt" → only "/" matches → dirA/file.txt.
	size, err = l.Query(ctx, "/other/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if size != int64(len(contentA)) {
		t.Fatalf("expected size %d (dirA), got %d", len(contentA), size)
	}
}

func TestLocal_Query_NoMatchingRoute(t *testing.T) {
	cfg := testLocalConfig{
		routes: map[string]string{
			"/foo/": "/does/not/matter/",
		},
	}

	l := filesize.NewLocal(cfg, zerolog.Nop())

	_, err := l.Query(context.Background(), "/bar/baz")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, filesize.ErrNoInfo) {
		t.Fatalf("expected ErrNoInfo, got %v", err)
	}
}
