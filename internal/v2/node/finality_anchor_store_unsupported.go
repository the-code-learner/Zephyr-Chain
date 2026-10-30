//go:build !linux && !darwin

package node

import "os"

func openLockedFinalityAnchor(string, bool) (*os.File, error) { return nil, ErrFinalityAnchor }
