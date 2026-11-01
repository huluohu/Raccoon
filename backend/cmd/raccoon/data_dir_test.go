package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckDataDirWritable_EmptyDir(t *testing.T) {
	if err := checkDataDirWritable(""); err != nil {
		t.Fatalf("empty dir should be a no-op, got %v", err)
	}
}

func TestCheckDataDirWritable_WritableDir(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "data")
	if err := checkDataDirWritable(target); err != nil {
		t.Fatalf("writable dir should succeed, got %v", err)
	}

	// Verify no probe file was left behind
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatalf("failed to read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected probe file to be removed, found %d entries", len(entries))
	}
}

func TestCheckDataDirWritable_TargetIsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := checkDataDirWritable(filePath); err == nil {
		t.Fatal("expected error when target is a file, got nil")
	}
}

func TestCheckDataDirWritable_ReadOnlyDir(t *testing.T) {
	// Skip on root because root ignores standard directory write permissions
	if os.Geteuid() == 0 {
		t.Skip("skipping read-only test as root")
	}

	dir := t.TempDir()
	roDir := filepath.Join(dir, "ro")
	if err := os.Mkdir(roDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(roDir, 0o755)
	})

	if err := checkDataDirWritable(roDir); err == nil {
		t.Fatal("expected error on read-only dir, got nil")
	}
}
