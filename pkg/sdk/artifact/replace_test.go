//go:build linux

package artifact

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// writeFile creates a file with exactly the given mode, so that assertions
// can rely on the mode rather than on the process umask.
func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// assertNoTemps fails when a temporary sibling was left behind.
func assertNoTemps(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".artifact-tmp-") {
			t.Errorf("leftover temporary entry %s", entry.Name())
		}
	}
}

func TestReplaceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloud-init.iso")
	writeFile(t, path, "original", 0o640)

	var before unix.Stat_t
	if err := unix.Lstat(path, &before); err != nil {
		t.Fatal(err)
	}

	err := ReplaceFile(context.Background(), path, func(tmpPath string) error {
		return os.WriteFile(tmpPath, []byte("patched"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}

	if data, err := os.ReadFile(path); err != nil {
		t.Fatal(err)
	} else if string(data) != "patched" {
		t.Errorf("content = %q, want %q", data, "patched")
	}
	var after unix.Stat_t
	if err := unix.Lstat(path, &after); err != nil {
		t.Fatal(err)
	}
	if after.Ino == before.Ino {
		t.Error("the replacement kept the original inode")
	}
	if mode := os.FileMode(after.Mode & 0o7777); mode != 0o640 {
		t.Errorf("mode = %04o, want 0640", mode)
	}
	assertNoTemps(t, dir)
}

func TestReplaceFileMutateError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	writeFile(t, path, "original", 0o644)

	mutateErr := errors.New("mutate failed")
	err := ReplaceFile(context.Background(), path, func(tmpPath string) error {
		return mutateErr
	})
	if !errors.Is(err, mutateErr) {
		t.Fatalf("error = %v, want %v", err, mutateErr)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Errorf("original = %q, %v; want %q", data, err, "original")
	}
	assertNoTemps(t, dir)
}

func TestReplaceFileContextCancelled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	writeFile(t, path, "original", 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	err := ReplaceFile(ctx, path, func(tmpPath string) error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want %v", err, context.Canceled)
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
		t.Errorf("original = %q, %v; want %q", data, err, "original")
	}
	assertNoTemps(t, dir)
}

func TestReplaceFileRejectsNonRegularTargets(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(subdir, link); err != nil {
		t.Fatal(err)
	}

	targets := map[string]string{
		"directory":     subdir,
		"symbolic link": link,
		"missing":       filepath.Join(dir, "missing"),
	}
	for name, path := range targets {
		if err := ReplaceFile(context.Background(), path, func(string) error { return nil }); err == nil {
			t.Errorf("replacing a %s succeeded", name)
		}
	}
}

func TestReplaceDir(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tree")
	if err := os.Mkdir(root, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o750); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "file"), "old", 0o640)
	writeFile(t, filepath.Join(sub, "nested"), "nested", 0o644)
	if err := os.Symlink("file", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}

	var before unix.Stat_t
	if err := unix.Lstat(root, &before); err != nil {
		t.Fatal(err)
	}

	err := ReplaceDir(context.Background(), root, func(tmpDir string) error {
		if err := os.Remove(filepath.Join(tmpDir, "file")); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(tmpDir, "added"), []byte("added"), 0o644); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tmpDir, "sub", "nested"), []byte("rewritten"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(filepath.Join(root, "file")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the removed entry still exists")
	}
	for path, want := range map[string]string{
		filepath.Join(root, "added"):         "added",
		filepath.Join(root, "sub", "nested"): "rewritten",
	} {
		if data, err := os.ReadFile(path); err != nil || string(data) != want {
			t.Errorf("%s = %q, %v; want %q", path, data, err, want)
		}
	}
	if target, err := os.Readlink(filepath.Join(root, "link")); err != nil || target != "file" {
		t.Errorf("link = %q, %v; want %q", target, err, "file")
	}
	var after unix.Stat_t
	if err := unix.Lstat(root, &after); err != nil {
		t.Fatal(err)
	}
	if after.Ino == before.Ino {
		t.Error("the replacement kept the original inode")
	}
	for path, want := range map[string]os.FileMode{
		root:                         0o750,
		sub:                          0o755,
		filepath.Join(sub, "nested"): 0o644,
	} {
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			t.Fatal(err)
		}
		if mode := os.FileMode(stat.Mode & 0o7777); mode != want {
			t.Errorf("mode of %s = %04o, want %04o", path, mode, want)
		}
	}
	assertNoTemps(t, dir)
}

func TestReplaceDirMutateError(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "tree")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "file"), "original", 0o644)

	mutateErr := errors.New("mutate failed")
	err := ReplaceDir(context.Background(), root, func(tmpDir string) error {
		if err := os.WriteFile(filepath.Join(tmpDir, "file"), []byte("changed"), 0o644); err != nil {
			return err
		}
		return mutateErr
	})
	if !errors.Is(err, mutateErr) {
		t.Fatalf("error = %v, want %v", err, mutateErr)
	}
	if data, err := os.ReadFile(filepath.Join(root, "file")); err != nil || string(data) != "original" {
		t.Errorf("original = %q, %v; want %q", data, err, "original")
	}
	assertNoTemps(t, dir)
}

func TestOwnershipPreserved(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("preserving another user's ownership requires root")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "artifact")
	writeFile(t, path, "original", 0o640)
	if err := os.Chown(path, 107, 107); err != nil {
		t.Fatal(err)
	}

	err := ReplaceFile(context.Background(), path, func(tmpPath string) error {
		return os.WriteFile(tmpPath, []byte("patched"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		t.Fatal(err)
	}
	if stat.Uid != 107 || stat.Gid != 107 {
		t.Errorf("owner = %d:%d, want 107:107", stat.Uid, stat.Gid)
	}
}
