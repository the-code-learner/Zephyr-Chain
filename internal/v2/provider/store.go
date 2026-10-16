package provider

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrStore = errors.New("compute provider content store error")

type DiskStore struct{ Dir string }

func (s DiskStore) Put(root types.Hash, data []byte) error {
	if s.Dir == "" || types.IsZero32([32]byte(root)) || ResultRoot(data) != root && InputRoot(data) != root {
		return ErrStore
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(s.Dir, root.String())
	tmp, err := os.CreateTemp(s.Dir, ".zephyr-provider-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (s DiskStore) Get(root types.Hash) ([]byte, error) {
	if s.Dir == "" || types.IsZero32([32]byte(root)) {
		return nil, ErrStore
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, root.String()))
	if err != nil {
		return nil, err
	}
	if InputRoot(data) != root && ResultRoot(data) != root {
		return nil, ErrStore
	}
	return data, nil
}
