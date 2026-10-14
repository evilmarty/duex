//go:build !windows

package analyzer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmlinkPolicy(t *testing.T) {
	const dataRoot = "/System/Volumes/Data"
	table := "Users\tUsers\n/System/Library/Caches\tSystem/Library/Caches\n\n"
	for _, tt := range []struct {
		root string
		want int
	}{
		{"/", 2},
		{"/System", 1},
		{dataRoot, 0},
		{"/Users", 0},
		{"/System/Volumes/Database", 0},
	} {
		t.Run(tt.root, func(t *testing.T) {
			p, err := firmlinkPolicy(tt.root, dataRoot, table)
			if err != nil {
				t.Fatal(err)
			}
			if len(p.aliases) != tt.want {
				t.Fatalf("aliases = %v, want %d entries", p.aliases, tt.want)
			}
			if p.excludes("/Users") || p.excludes(dataRoot+"/Users-extra") {
				t.Fatal("excluded a canonical path or a non-alias sibling")
			}
		})
	}
	for _, table := range []string{"invalid", "/Users Users extra", "../Users Users", "/Users /Users", "/Users ../Users", "/Users .", ". Users"} {
		t.Run(table, func(t *testing.T) {
			if _, err := firmlinkPolicy("/", dataRoot, table); err == nil {
				t.Fatal("expected an invalid table error")
			}
		})
	}
}

func TestAnalyzeFirmlinks(t *testing.T) {
	root := t.TempDir()
	dataRoot := filepath.Join(root, "System", "Volumes", "Data")
	paths := []string{
		filepath.Join(root, "Users", "user.txt"),
		filepath.Join(dataRoot, "Users", "user.txt"),
		filepath.Join(dataRoot, "private-data", "unique.bin"),
		filepath.Join(root, "external", "external.log"),
	}
	sizes := make([]int64, len(paths))
	for i, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(strings.Repeat("data", 1024)), 0644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		sizes[i] = getFileStats(info).Size
	}
	policy, err := firmlinkPolicy(root, dataRoot, filepath.Join(root, "Users")+"\tUsers\n")
	if err != nil {
		t.Fatal(err)
	}
	dataDev := uint64(2)
	policy.dataDev = &dataDev
	origPolicy, origStats, origReadDir := loadScanPolicy, getFileStats, readDir
	t.Cleanup(func() {
		loadScanPolicy, getFileStats, readDir = origPolicy, origStats, origReadDir
	})
	loadScanPolicy = func(path string, oneFileSystem bool) (scanPolicy, error) {
		if path == root {
			return policy, nil
		}
		if path == filepath.Join(root, "System") {
			return scanPolicy{dataDev: &dataDev}, nil
		}
		return scanPolicy{}, nil
	}
	getFileStats = func(info os.FileInfo) fileStats {
		stats := origStats(info)
		stats.Dev = 1
		switch info.Name() {
		case "Data", "Users", "private-data":
			stats.Dev = 2
		case "external":
			stats.Dev = 3
		}
		return stats
	}
	readDir = func(path string) ([]os.DirEntry, error) {
		if path == filepath.Join(dataRoot, "Users") {
			return nil, errors.New("backing alias must not be traversed")
		}
		return origReadDir(path)
	}
	cache := map[string]Result{
		filepath.Join(root, "Users"):  {TotalSize: 999999},
		filepath.Join(root, "System"): {TotalSize: 999999},
		dataRoot:                      {TotalSize: 999999},
	}
	for _, oneFS := range []bool{true, false} {
		for range 5 {
			res, err := Analyze(context.Background(), root, nil, cache, oneFS, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			expected := sizes[0] + sizes[2]
			topCount := 2
			if !oneFS {
				expected += sizes[3]
				topCount++
			}
			if res.TotalSize != expected || len(res.TopFiles) != topCount {
				t.Fatalf("oneFS=%v: total=%d top files=%v, want total=%d files=%d", oneFS, res.TotalSize, res.TopFiles, expected, topCount)
			}
			if res.ExcludedAliases != 1 || res.ErrorsCount != 0 {
				t.Fatalf("aliases=%d errors=%d, want 1 alias and no errors", res.ExcludedAliases, res.ErrorsCount)
			}
			var breakdownSize int64
			for _, b := range res.Breakdown {
				breakdownSize += b.Size
			}
			if breakdownSize != expected {
				t.Fatalf("breakdown size=%d, want %d", breakdownSize, expected)
			}
			for _, f := range res.Files {
				if f.Name == "System" && (f.Size != sizes[2] || f.ExcludedAliases != 1) {
					t.Fatalf("System contribution includes alias: %+v", f)
				}
			}
			for _, f := range res.TopFiles {
				if f.Path == paths[1] {
					t.Fatalf("backing alias leaked into top files: %s", f.Path)
				}
			}
		}
	}
	readDir = origReadDir
	res, err := Analyze(context.Background(), filepath.Join(root, "System"), nil, cache, true, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalSize != sizes[1]+sizes[2] || res.ExcludedAliases != 0 {
		t.Fatalf("navigation through System must still include Data: %+v", res)
	}
	res, err = Analyze(context.Background(), dataRoot, nil, nil, true, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalSize != sizes[1]+sizes[2] || res.ExcludedAliases != 0 {
		t.Fatalf("direct Data scan should include all its data: %+v", res)
	}
}

func TestAnalyzePolicyError(t *testing.T) {
	orig := loadScanPolicy
	t.Cleanup(func() { loadScanPolicy = orig })
	want := errors.New("cannot read firmlink table")
	loadScanPolicy = func(string, bool) (scanPolicy, error) {
		return scanPolicy{}, want
	}
	if _, err := Analyze(context.Background(), t.TempDir(), nil, nil, true, nil, 0); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}
