//go:build !linux

package artifact

import (
	"context"
	"errors"
	"fmt"
)

// ReplaceFile is only implemented on Linux; elsewhere it returns
// errors.ErrUnsupported.
func ReplaceFile(_ context.Context, path string, _ func(tmpPath string) error) error {
	return fmt.Errorf("replace %s: %w", path, errors.ErrUnsupported)
}

// ReplaceDir is only implemented on Linux; elsewhere it returns
// errors.ErrUnsupported.
func ReplaceDir(_ context.Context, path string, _ func(tmpDir string) error) error {
	return fmt.Errorf("replace %s: %w", path, errors.ErrUnsupported)
}
