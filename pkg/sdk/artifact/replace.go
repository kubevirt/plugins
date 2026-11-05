//go:build linux

// Package artifact atomically replaces the boot artifacts KubeVirt passes to
// PreBoot launcher hooks: the caller modifies a private copy of the artifact,
// which is then swapped in. See the README for PreBoot authoring guidance.
package artifact

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// ReplaceFile copies the regular file at path, lets mutate modify the copy
// and, if mutate succeeds and ctx is still live, atomically replaces path
// with it. On any error the original is untouched and the copy removed.
func ReplaceFile(ctx context.Context, path string, mutate func(tmpPath string) error) error {
	return replace(ctx, path, false, mutate)
}

// ReplaceDir copies the directory tree at path, lets mutate modify the copy
// and, if mutate succeeds and ctx is still live, swaps it in atomically with
// renameat2(2) RENAME_EXCHANGE. On any error the original is untouched and
// the copy removed. The tree may hold directories, regular files and symlinks.
func ReplaceDir(ctx context.Context, path string, mutate func(tmpDir string) error) error {
	return replace(ctx, path, true, mutate)
}

func replace(ctx context.Context, path string, dir bool, mutate func(tmpPath string) error) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if dir && !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if !dir && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	tmpDir, err := os.MkdirTemp(filepath.Dir(path), "."+filepath.Base(path)+".artifact-tmp-*")
	if err != nil {
		return err
	}
	// Removes the failed copy, or after the exchange the old tree.
	defer os.RemoveAll(tmpDir)
	// The copy keeps the original basename, so the hook sees a familiar path.
	copyPath := filepath.Join(tmpDir, filepath.Base(path))
	if err := copyEntry(path, copyPath, info); err != nil {
		return err
	}
	if err := mutate(copyPath); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// After mutate, so the hook cannot change the root's mode or owner.
	if err := copyMetadata(info, copyPath); err != nil {
		return err
	}
	if dir {
		return unix.Renameat2(unix.AT_FDCWD, copyPath, unix.AT_FDCWD, path, unix.RENAME_EXCHANGE)
	}
	return os.Rename(copyPath, path)
}

// copyEntry copies the file, symlink or directory tree at src to dst, which
// must not exist, and restores the original's mode and ownership.
func copyEntry(src, dst string, info os.FileInfo) error {
	switch {
	case info.IsDir():
		if err := os.Mkdir(dst, 0o700); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			entryInfo, err := entry.Info()
			if err != nil {
				return err
			}
			srcPath, dstPath := filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())
			if err := copyEntry(srcPath, dstPath, entryInfo); err != nil {
				return err
			}
		}
	case info.Mode().IsRegular():
		source, err := os.Open(src)
		if err != nil {
			return err
		}
		defer source.Close()
		target, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(target, source)
		if err := errors.Join(copyErr, target.Close()); err != nil {
			return err
		}
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.Symlink(target, dst); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%s is neither a directory, a regular file nor a symbolic link", src)
	}
	// A directory's metadata lands after its contents, so a read-only source
	// mode cannot block copying the contents below it.
	return copyMetadata(info, dst)
}

// copyMetadata makes dst carry the ownership and permission bits recorded in
// info. A failed chown is an error, not a warning: ownership decides whether
// QEMU (uid 107) can use the artifact. Chown runs before chmod because it can
// clear setuid and setgid bits.
func copyMetadata(info os.FileInfo, dst string) error {
	stat := info.Sys().(*syscall.Stat_t)
	if err := os.Lchown(dst, int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return os.Chmod(dst, info.Mode())
	}
	return nil
}
