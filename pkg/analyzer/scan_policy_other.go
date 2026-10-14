//go:build !darwin

package analyzer

var loadScanPolicy = func(root string, oneFileSystem bool) (scanPolicy, error) {
	return scanPolicy{}, nil
}
