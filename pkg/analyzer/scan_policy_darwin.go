package analyzer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var readFirmlinks = os.ReadFile
var statVolume = os.Stat

var loadScanPolicy = func(root string, oneFileSystem bool) (scanPolicy, error) {
	scanRoot := root
	root, err := filepath.Abs(root)
	if err != nil {
		return scanPolicy{}, err
	}
	const dataRoot = "/System/Volumes/Data"
	if !withinPath(root, dataRoot) || root == dataRoot {
		return scanPolicy{}, nil
	}
	table, err := readFirmlinks("/usr/share/firmlinks")
	if errors.Is(err, os.ErrNotExist) {
		// macOS versions before Catalina have no System/Data volume split.
		return scanPolicy{}, nil
	}
	if err != nil {
		return scanPolicy{}, fmt.Errorf("read macOS firmlinks: %w", err)
	}
	p, err := firmlinkPolicy(root, dataRoot, string(table))
	if err != nil {
		return scanPolicy{}, err
	}
	aliases := make(map[string]string, len(p.aliases))
	for target, source := range p.aliases {
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return scanPolicy{}, err
		}
		aliases[filepath.Join(scanRoot, rel)] = source
	}
	p.aliases = aliases
	if oneFileSystem {
		info, err := statVolume(dataRoot)
		if err != nil {
			return scanPolicy{}, fmt.Errorf("stat macOS Data volume: %w", err)
		}
		dev := getFileStats(info).Dev
		p.dataDev = &dev
	}
	return p, nil
}
