package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"sf-typings/internal/generator"
)

// inspectOutputDir checks every existing component without following symlinks.
func inspectOutputDir(dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("cannot inspect output directory")
	}
	for {
		info, err := os.Lstat(dir)
		if err == nil && !info.IsDir() {
			return fmt.Errorf("unsafe output directory")
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect output directory")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

func save(dir string, files []generator.File) error {
	if err := inspectOutputDir(dir); err != nil {
		return err
	}
	// Validate ALL directories and targets before staging or publishing anything.
	// In particular, a later sobjects/ symlink must not permit an earlier Apex write.
	for _, f := range files {
		if !filepath.IsLocal(f.Name) || filepath.ToSlash(filepath.Clean(f.Name)) != filepath.ToSlash(f.Name) || f.Name == "." {
			return fmt.Errorf("unsafe output target")
		}
		target := filepath.Join(dir, f.Name)
		if err := inspectOutputDir(filepath.Dir(target)); err != nil {
			return err
		}
		info, err := os.Lstat(target)
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe output target")
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect output target")
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create output directory")
	}
	for _, f := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f.Name)), 0700); err != nil {
			return fmt.Errorf("cannot create output directory")
		}
	}
	type staged struct{ target, temp string }
	var pending []staged
	defer func() {
		for _, item := range pending {
			os.Remove(item.temp)
		}
	}()
	for _, f := range files {
		tmp, err := os.CreateTemp(dir, ".sf-typings-*")
		if err != nil {
			return fmt.Errorf("cannot stage output")
		}
		pending = append(pending, staged{filepath.Join(dir, f.Name), tmp.Name()})
		if _, err = tmp.Write(f.Data); err != nil {
			tmp.Close()
			return fmt.Errorf("cannot stage output")
		}
		if err = tmp.Close(); err != nil {
			return fmt.Errorf("cannot stage output")
		}
	}
	for _, item := range pending {
		if err := os.Rename(item.temp, item.target); err != nil {
			return fmt.Errorf("cannot publish output")
		}
	}
	return nil
}
