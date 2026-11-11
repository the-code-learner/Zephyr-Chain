//go:build !linux && !darwin

package node

import "os"

func openLockedFinalityAnchor(string, bool) (*os.File, error) { return nil, ErrFinalityAnchor }

// No unsupported-platform production signing anchor may claim inode safety.
func singleLinkFinalityAnchor(os.FileInfo) bool { return false }
func validFinalityAnchorFile(*os.File) bool { return false }
