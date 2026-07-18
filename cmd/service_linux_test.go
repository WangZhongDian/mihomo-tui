//go:build linux

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallBinaryAtomicallyReplacesExistingFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(source, []byte("new binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old binary"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := installBinaryAtomically(source, target); err != nil {
		t.Fatalf("install: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new binary" {
		t.Fatalf("target = %q", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("mode = %04o, want 0755", info.Mode().Perm())
	}
}
