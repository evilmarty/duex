package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyze(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "dude-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create some files
	file1 := filepath.Join(tmpDir, "file1.txt")
	if err := os.WriteFile(file1, []byte("hello"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	file2 := filepath.Join(subDir, "file2.txt")
	if err := os.WriteFile(file2, []byte("world!"), 0644); err != nil {
		t.Fatalf("Failed to create file2: %v", err)
	}

	result, err := Analyze(tmpDir)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	expectedTotal := int64(len("hello") + len("world!"))
	if result.TotalSize != expectedTotal {
		t.Errorf("Expected total size %d, got %d", expectedTotal, result.TotalSize)
	}
}
