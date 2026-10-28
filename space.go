package main

import "path/filepath"

// Match the preflight's conservative free-space check, including the case
// where the common directory is mounted on a different filesystem.
func destinationSpace(path string) (uint64, error) {
	target, err := appsPath(path)
	if err != nil {
		return 0, err
	}
	base, err := existingBase(target)
	if err != nil {
		return 0, err
	}
	common, err := existingBase(filepath.Join(target, "common"))
	if err != nil {
		return 0, err
	}
	a, err := freeBytes(base)
	if err != nil {
		return 0, err
	}
	b, err := freeBytes(common)
	if err != nil {
		return 0, err
	}
	if b < a {
		return b, nil
	}
	return a, nil
}
