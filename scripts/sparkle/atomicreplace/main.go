package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: atomicreplace <staging-dir> <target-dir>")
		os.Exit(64)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "atomicreplace:", err)
		os.Exit(1)
	}
}

func run(source, target string) error {
	source, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if filepath.Dir(source) != filepath.Dir(target) || filepath.Base(target) != "update" || !strings.HasPrefix(filepath.Base(source), ".update.staging.") {
		return errors.New("source and target must be release directories in the same dist directory")
	}
	if err := requireDirectory(source); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return os.Rename(source, target)
	} else if err != nil {
		return err
	}
	if err := requireDirectory(target); err != nil {
		return fmt.Errorf("target: %w", err)
	}
	return atomicSwap(source, target)
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("path must be a real directory")
	}
	return nil
}
