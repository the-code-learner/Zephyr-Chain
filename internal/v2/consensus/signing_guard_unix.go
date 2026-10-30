//go:build linux || darwin

package consensus

import (
	"errors"
	"os"
	"syscall"
)

func openLockedSigningJournal(path string) (*os.File, bool, error) {
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	fd, err := syscall.Open(path, flags|syscall.O_CREAT|syscall.O_EXCL, 0600)
	created := err == nil
	if errors.Is(err, syscall.EEXIST) {
		fd, err = syscall.Open(path, flags, 0)
	}
	if err != nil {
		return nil, false, ErrSigningGuard
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = syscall.Close(fd)
		return nil, false, ErrSigningGuard
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		_ = file.Close()
		return nil, false, ErrSigningGuard
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, false, ErrSigningGuardBusy
	}
	return file, created, nil
}
