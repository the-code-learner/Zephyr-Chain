package ledger

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomicReplacesContentAndRestrictsPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatalf("seed state file: %v", err)
	}

	if err := writeFileAtomic(path, []byte("new state"), 0o600); err != nil {
		t.Fatalf("atomic write: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if string(content) != "new state" {
		t.Fatalf("expected replacement content, got %q", string(content))
	}

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("expected state permissions 0600, got %04o", permissions)
	}
}
