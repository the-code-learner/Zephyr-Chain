//go:build !linux && !darwin

package worldstate

import "os"

// Production durable state must never silently downgrade to an unlocked
// multi-writer store on platforms without an audited OS process lock.
func acquireWriterLock(string) (*os.File, error) {
	return nil, ErrPersistenceUnsupported
}

func releaseWriterLock(file *os.File) error {
	if file == nil {
		return nil
	}
	return file.Close()
}
