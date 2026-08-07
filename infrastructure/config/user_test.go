package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteUserFieldRestrictsExistingConfigPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(path, []byte("api_key: old\n"), 0644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	if err := WriteUserField(path, "api_key", "new"); err != nil {
		t.Fatalf("WriteUserField: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf("config mode = %04o, want 0600", got)
	}
}
