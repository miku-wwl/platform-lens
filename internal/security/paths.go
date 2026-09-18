package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var ErrPathOutsideWorkspace = errors.New("path outside workspace")

func ResolveExistingWithin(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", ErrPathOutsideWorkspace
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(relative)
	candidate := filepath.Join(rootAbs, clean)
	if !within(rootAbs, candidate) {
		return "", ErrPathOutsideWorkspace
	}
	candidate, err = filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", err
	}
	if !within(rootAbs, candidate) {
		return "", ErrPathOutsideWorkspace
	}
	return candidate, nil
}

func ResolveOutputWithin(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", ErrPathOutsideWorkspace
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs, err = filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(relative)
	parent, err := filepath.EvalSymlinks(filepath.Join(rootAbs, filepath.Dir(clean)))
	if err != nil {
		return "", err
	}
	if !within(rootAbs, parent) {
		return "", ErrPathOutsideWorkspace
	}
	return filepath.Join(parent, filepath.Base(clean)), nil
}

func within(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && !filepath.IsAbs(rel)
}
