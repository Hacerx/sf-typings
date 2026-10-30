package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"sf-typings/internal/generator"
)

func save(dir string, files []generator.File) error {
	if info, err := os.Lstat(dir); err == nil && !info.IsDir() {
		return fmt.Errorf("unsafe output directory")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect output directory")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create output directory")
	}
	// Reject non-regular targets before touching any generated file.
	for _, f := range files {
		info, err := os.Lstat(filepath.Join(dir, f.Name))
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe output target")
		}
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot inspect output target")
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
