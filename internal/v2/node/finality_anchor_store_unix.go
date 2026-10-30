//go:build linux || darwin

package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func openLockedFinalityAnchor(path string, create bool) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrFinalityAnchor
	}
	// Operator must provision an isolated, private directory on a different
	// backup/failure domain. Do not create a parent directory implicitly.
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("%w: inspect parent directory: %v", ErrFinalityAnchor, err)
	}
	if !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return nil, fmt.Errorf("%w: anchor parent must be an owner-only real directory (mode %v)", ErrFinalityAnchor, parent.Mode())
	}
	flags := syscall.O_RDWR | syscall.O_APPEND | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if create {
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path, flags, 0600)
	if err != nil {
		return nil, errors.Join(ErrFinalityAnchor, err)
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, ErrFinalityAnchor
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("%w: anchor descriptor stat: %v", ErrFinalityAnchor, err)
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: anchor file must be regular mode 0600 (mode %v)", ErrFinalityAnchor, stat.Mode())
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.Join(ErrFinalityAnchor, err)
	}
	return file, nil
}
