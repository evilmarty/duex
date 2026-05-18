package analyzer

import (
	"context"
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

	result, err := Analyze(context.Background(),tmpDir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	// Calculate expected physical size
	var expectedTotal int64
	for _, path := range []string{file1, file2} {
		info, _ := os.Stat(path)
		expectedTotal += getFileStats(info).Size
	}

	if result.TotalSize != expectedTotal {
		t.Errorf("Expected total size %d, got %d", expectedTotal, result.TotalSize)
	}

	// Verify breakdown for subdir
	foundSubdir := false
	for _, f := range result.Files {
		if f.Name == "subdir" {
			foundSubdir = true
			if len(f.Breakdown) == 0 {
				t.Error("Expected breakdown for subdir, got empty")
			}
			foundTxt := false
			for _, b := range f.Breakdown {
				if b.Extension == ".txt" {
					foundTxt = true
					info, _ := os.Stat(file2)
					expected := getFileStats(info).Size
					if b.Size != expected {
						t.Errorf("Expected .txt size %d for subdir, got %d", expected, b.Size)
					}
				}
			}
			if !foundTxt {
				t.Error("Expected .txt in subdir breakdown")
			}
		}
	}
	if !foundSubdir {
		t.Error("Expected to find 'subdir' in results")
	}

	// Verify aggregate breakdown
	if len(result.Breakdown) == 0 {
		t.Error("Expected root aggregate breakdown, got empty")
	}
	foundTxtAtRoot := false
	for _, b := range result.Breakdown {
		if b.Extension == ".txt" {
			foundTxtAtRoot = true
			if b.Size != expectedTotal {
				t.Errorf("Expected root breakdown .txt size %d, got %d", expectedTotal, b.Size)
			}
		}
	}
	if !foundTxtAtRoot {
		t.Error("Expected .txt in root aggregate breakdown")
	}
}

func TestAnalyzeHardLinks(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "dude-hardlink-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file
	file1 := filepath.Join(tmpDir, "file1.txt")
	content := []byte("shared content")
	if err := os.WriteFile(file1, content, 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	// Create a hard link
	file2 := filepath.Join(tmpDir, "file1_link.txt")
	if err := os.Link(file1, file2); err != nil {
		t.Skip("Hard links not supported on this filesystem")
	}

	result, err := Analyze(context.Background(),tmpDir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// It should find 2 entries
	if len(result.Files) != 2 {
		t.Errorf("Expected 2 files, got %d", len(result.Files))
	}

	// But the total size should only be the physical size of ONE file
	info, _ := os.Stat(file1)
	expectedSize := getFileStats(info).Size

	if result.TotalSize != expectedSize {
		t.Errorf("Expected total size %d (counted once), got %d", expectedSize, result.TotalSize)
	}
}

func TestAnalyzeLongExtension(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "dude-longext-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a file with an excessively long extension
	longExt := ".a0~__CX9j0QIqNd7exyZ5zPlE5EeM6jzt86awZCKR-eN68wV7qfj5P60gacfUh7oVojv9yXCYXkP7JcIuyx3AdRXg=="
	fileName := "data" + longExt
	filePath := filepath.Join(tmpDir, fileName)
	content := []byte("some content")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatalf("Failed to create file with long extension: %v", err)
	}

	result, err := Analyze(context.Background(),tmpDir, nil)
	if err != nil {
		t.Fatalf("Analyze failed: %v", err)
	}

	// Verify breakdown grouping
	// The file should be grouped under "Other"
	foundOther := false

	// Revised approach: check breakdown of a subdirectory containing the long extension file
	subDir := filepath.Join(tmpDir, "sub")
	os.Mkdir(subDir, 0755)
	os.WriteFile(filepath.Join(subDir, "file"+longExt), content, 0644)

	result, _ = Analyze(context.Background(),tmpDir, nil)
	for _, f := range result.Files {
		if f.Name == "sub" {
			for _, b := range f.Breakdown {
				if b.Extension == "Other" {
					foundOther = true
				}
				if b.Extension == longExt {
					t.Errorf("Should NOT find long extension %s in breakdown", longExt)
				}
			}
		}
	}

	if !foundOther {
		t.Error("Expected to find 'Other' in breakdown for file with long extension")
	}
}
