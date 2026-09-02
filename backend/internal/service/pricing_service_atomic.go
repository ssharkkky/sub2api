package service

import (
	"os"
	"path/filepath"
)

// writeAtomic 通过「同目录临时文件 + 写入 + fsync + rename」落盘，
// 保证轮询层/其他进程永远读不到半截文件。
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		tmpName = ""
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		tmpName = ""
		return err
	}
	if err := tmp.Close(); err != nil {
		tmpName = ""
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		tmpName = ""
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}
