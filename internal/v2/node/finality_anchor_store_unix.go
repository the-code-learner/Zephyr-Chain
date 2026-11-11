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
	// A second hard-link name bypasses the intended single canonical anchor
	// pathname and can expose the local journal to unreviewed backup/restore
	// paths. O_NOFOLLOW only denies symlinks, not hard links.
	if !singleLinkFinalityAnchor(stat) {
		_ = file.Close()
		return nil, ErrFinalityAnchor
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 {
		_ = file.Close()
		return nil, fmt.Errorf("%w: anchor file must be regular mode 0600 (mode %v)", ErrFinalityAnchor, stat.Mode())
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, errors.Join(ErrFinalityAnchor, err)
	}
	// The canonical path must still identify this locked inode after
	// obtaining the lock. A concurrent rename/replace cannot silently
	// redirect subsequent recovery to a different anchor file.
	if !validFinalityAnchorFile(file) {
		_ = file.Close()
		return nil, ErrFinalityAnchor
	}
	return file, nil
}

// singleLinkFinalityAnchor is a local inode invariant, NOT a global
// monotonic HSM counter or a guarantee against full-file rollback.
func singleLinkFinalityAnchor(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

// validFinalityAnchorFile binds the held descriptor to its canonical path.
// An unlinked, renamed, replaced, hard-linked or symlink-replaced journal is
// unsafe for further high-water reads or signing. The check is best-effort
// local filesystem protection, NOT a remote nonrollbackable authority.
func validFinalityAnchorFile(file *os.File) bool {
	if file == nil {
		return false
	}
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Mode().Perm() != 0600 || !singleLinkFinalityAnchor(opened) {
		return false
	}
	canonical, err := os.Lstat(file.Name())
	return err == nil && canonical.Mode().IsRegular() &&
		canonical.Mode().Perm() == 0600 && singleLinkFinalityAnchor(canonical) &&
		os.SameFile(opened, canonical)
}
