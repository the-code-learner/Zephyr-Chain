package ledger

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".zephyr-state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state file: %w", err)
	}

	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set temporary state permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("write temporary state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync temporary state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary state: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace state file: %w", err)
	}
	removeTemporary = false

	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("set state permissions: %w", err)
	}

	// Windows does not expose the same durable-directory fsync semantics as
	// Unix-like systems. The file itself has already been synced before rename.
	if runtime.GOOS == "windows" {
		return nil
	}

	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open state directory for sync: %w", err)
	}
	if syncErr := directoryHandle.Sync(); syncErr != nil {
		_ = directoryHandle.Close()
		return fmt.Errorf("sync state directory: %w", syncErr)
	}
	if closeErr := directoryHandle.Close(); closeErr != nil {
		return fmt.Errorf("close state directory: %w", closeErr)
	}

	return nil
}
