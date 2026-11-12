//go:build linux || darwin

package consensus

import (
	"errors"
	"os"
	"syscall"
)

// validSigningJournalFile refuses a swapped pathname, duplicate hardlink,
// symlink or exposed journal. A flock protects ONLY an inode: it does not
// protect the pathname from unlink/rename/replace by another host process.
func validSigningJournalFile(file *os.File) bool {
	if file == nil {
		return false
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm()&0077 != 0 {
		return false
	}
	inode, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || inode.Nlink != 1 {
		return false
	}
	pathInfo, err := os.Lstat(file.Name())
	return err == nil && pathInfo.Mode().IsRegular() &&
		pathInfo.Mode().Perm()&0077 == 0 && os.SameFile(opened, pathInfo)
}

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
	if !validSigningJournalFile(file) {
		_ = file.Close()
		return nil, false, ErrSigningGuard
	}
	return file, created, nil
}
