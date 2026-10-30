//go:build !linux && !darwin

package consensus

import "os"

func openLockedSigningJournal(string) (*os.File, bool, error) {
	return nil, false, ErrSigningGuardUnsupported
}
