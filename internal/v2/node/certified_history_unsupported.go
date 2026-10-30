//go:build !linux && !darwin

package node

import "os"

func openLockedCertifiedHistory(string, bool) (*os.File, error) {
	return nil, ErrCertifiedHistory
}

func syncHistoryDirectory(string) error { return ErrCertifiedHistory }
