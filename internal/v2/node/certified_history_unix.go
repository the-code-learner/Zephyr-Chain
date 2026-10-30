//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func openLockedCertifiedHistory(path string, create bool) (*os.File, error) {
	flags := syscall.O_RDWR | syscall.O_APPEND | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if create {
		flags |= syscall.O_CREAT | syscall.O_EXCL
	}
	fd, err := syscall.Open(path, flags, 0600)
	if err != nil {
		return nil, errors.Join(ErrCertifiedHistory, err)
	}
	f := os.NewFile(uintptr(fd), path)
	if f == nil {
		_ = syscall.Close(fd)
		return nil, ErrCertifiedHistory
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		_ = f.Close()
		return nil, ErrCertifiedHistory
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, errors.Join(ErrCertifiedHistory, err)
	}
	return f, nil
}

func syncHistoryDirectory(path string) error {
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
