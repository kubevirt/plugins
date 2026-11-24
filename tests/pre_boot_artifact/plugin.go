package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"golang.org/x/sys/unix"

	"github.com/kubevirt/plugins/pkg/sdk/artifact"
	"github.com/kubevirt/plugins/pkg/sdk/plugin"
)

// The markers are deliberately the same length: replacing one with the other
// byte for byte keeps the ISO9660 image valid, the same approach KubeVirt's
// own e2e sidecar uses.
const (
	markerBefore = "SDKP4_PREBOOT_BEFORE"
	markerAfter  = "SDKP4_PREBOOT_AFTER!"
)

type preBootArtifactHook struct{}

func (*preBootArtifactHook) PreBoot(ctx context.Context, req *plugin.PreBootRequest) (*plugin.PreBootResponse, error) {
	isoPath, ok := req.CloudInitISO()
	if !ok {
		return nil, fmt.Errorf("PreBoot request has no cloudInitISO artifact")
	}

	if err := logFileIdentity("PreBoot original", isoPath, ""); err != nil {
		return nil, err
	}

	mutated := false
	err := artifact.ReplaceFile(ctx, isoPath, func(tmpPath string) error {
		data, err := os.ReadFile(tmpPath)
		if err != nil {
			return err
		}
		if bytes.Contains(data, []byte(markerAfter)) && !bytes.Contains(data, []byte(markerBefore)) {
			// PreBoot may repeat after a failed start; treat an already
			// mutated artifact as success so the hook stays idempotent.
			return nil
		}
		if !bytes.Contains(data, []byte(markerBefore)) {
			return fmt.Errorf("cloud-init ISO contains neither %q nor %q", markerBefore, markerAfter)
		}
		mutated = true
		if err := os.WriteFile(tmpPath, bytes.ReplaceAll(data, []byte(markerBefore), []byte(markerAfter)), 0o600); err != nil {
			return err
		}
		// Keep the copy at 0600 so only the helper's metadata restore can
		// bring the original mode back.
		return os.Chmod(tmpPath, 0o600)
	})
	if err != nil {
		log.Printf("PreBoot ReplaceFile error: %v", err)
		return nil, err
	}
	return nil, logFileIdentity("PreBoot replaced", isoPath, fmt.Sprintf(" mutated=%t", mutated))
}

// logFileIdentity logs the file's owner, permission bits, SELinux label and
// inode so the functional test can verify that ReplaceFile swapped in a new
// inode while preserving the file's identity. suffix carries extra fields
// for the line.
func logFileIdentity(prefix, path, suffix string) error {
	var stat unix.Stat_t
	if err := unix.Lstat(path, &stat); err != nil {
		return fmt.Errorf("%s: lstat %s: %w", prefix, path, err)
	}
	label, err := fileLabel(path)
	if err != nil {
		return fmt.Errorf("%s: read security.selinux of %s: %w", prefix, path, err)
	}
	log.Printf("%s path=%s uid=%d gid=%d mode=%04o label=%s ino=%d%s", prefix, path, stat.Uid, stat.Gid, stat.Mode&0o7777, label, stat.Ino, suffix)
	return nil
}

// fileLabel returns the SELinux label of path, "<none>" when the file has no
// label or the filesystem does not support security.selinux, and an error
// for any other failure.
func fileLabel(path string) (string, error) {
	buf := make([]byte, 4096)
	n, err := unix.Lgetxattr(path, "security.selinux", buf)
	if errors.Is(err, unix.ENODATA) || errors.Is(err, unix.ENOTSUP) {
		return "<none>", nil
	}
	if err != nil {
		return "", err
	}
	return string(bytes.TrimRight(buf[:n], "\x00")), nil
}

func newPlugin() *plugin.Plugin {
	return plugin.New("test-pre-boot-artifact").
		WithLauncherHook(plugin.PreBoot(&preBootArtifactHook{}).WithFailureStrategy(plugin.Fail))
}

func main() {
	newPlugin().Execute()
}
