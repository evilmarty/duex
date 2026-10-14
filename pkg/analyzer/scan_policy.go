package analyzer

import (
	"fmt"
	"path/filepath"
	"strings"
)

type scanPolicy struct {
	aliases map[string]string
	dataDev *uint64
}

func (p scanPolicy) allowsDevice(dev, rootDev uint64) bool {
	return dev == rootDev || (p.dataDev != nil && dev == *p.dataDev)
}

func (p scanPolicy) excludes(path string) bool {
	if len(p.aliases) == 0 {
		return false
	}
	_, ok := p.aliases[filepath.Clean(path)]
	return ok
}

func firmlinkPolicy(root, dataRoot, table string) (scanPolicy, error) {
	p := scanPolicy{aliases: make(map[string]string)}
	for _, line := range strings.Split(table, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 2 {
			return scanPolicy{}, fmt.Errorf("invalid firmlink entry: %q", line)
		}
		source := filepath.Clean(fields[0])
		target := filepath.Clean(fields[1])
		if source == "." || source == ".." || strings.HasPrefix(source, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(target) || target == "." || target == ".." || strings.HasPrefix(target, ".."+string(filepath.Separator)) {
			return scanPolicy{}, fmt.Errorf("invalid firmlink paths: %q", line)
		}
		if !filepath.IsAbs(source) {
			source = filepath.Join(string(filepath.Separator), source)
		}
		target = filepath.Join(dataRoot, target)
		if withinPath(root, source) && withinPath(root, target) && source != root && target != root {
			p.aliases[target] = source
		}
	}
	return p, nil
}

func withinPath(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
