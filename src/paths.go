package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Make an override absolute without cleaning symlink/.. before resolution.
func absoluteRawPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return cwd + string(filepath.Separator) + path, nil
}

// Resolve only the parent: callers must still reject or replace a final symlink,
// rather than silently following it to overwrite a different file.
func absoluteOutputPath(path string) (string, error) {
	path, err := absoluteRawPath(path)
	if err != nil {
		return "", err
	}
	parent, name := filepath.Split(path)
	parent, err = canonicalProcessPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, name), nil
}

// 既有路徑按 inode 比對；未建立的子路徑遵守所在磁碟的大小寫規則。
func sameFilesystemPath(a, b string) (bool, error) {
	a, err := canonicalProcessPath(a)
	if err != nil {
		return false, err
	}
	b, err = canonicalProcessPath(b)
	if err != nil {
		return false, err
	}
	return sameCanonicalPath(a, b)
}

func sameCanonicalPath(a, b string) (bool, error) {
	if a == b {
		return true, nil
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	if ae != nil && !errors.Is(ae, os.ErrNotExist) {
		return false, ae
	}
	if be != nil && !errors.Is(be, os.ErrNotExist) {
		return false, be
	}
	if ae == nil && be == nil {
		return os.SameFile(ai, bi), nil
	}
	if ae == nil || be == nil {
		return false, nil
	}
	ap, bp := filepath.Dir(a), filepath.Dir(b)
	if ap == a || bp == b {
		return false, nil
	}
	sameParent, err := sameCanonicalPath(ap, bp)
	if err != nil || !sameParent {
		return false, err
	}
	an, bn := filepath.Base(a), filepath.Base(b)
	if an == bn {
		return true, nil
	}
	for {
		_, err = os.Stat(ap)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrNotExist) || filepath.Dir(ap) == ap {
			return false, err
		}
		ap = filepath.Dir(ap)
	}
	return filesystemNamesEqual(ap, an, bn)
}

// Unicode normalization and folding differ between APFS, HFS+, and HFSX.
// For missing Unicode names, let this volume resolve an isolated empty file
// rather than guessing from the runtime's Unicode tables. Failure is closed.
func filesystemNamesEqual(parent, a, b string) (same bool, err error) {
	ascii := true
	for _, c := range []byte(a + b) {
		if c >= utf8.RuneSelf {
			ascii = false
			break
		}
	}
	if ascii {
		if !strings.EqualFold(a, b) {
			return false, nil
		}
		sensitive, err := filesystemCaseSensitive(parent)
		return !sensitive, err
	}
	probe, err := os.MkdirTemp(parent, ".codexswitch-path-*")
	if err != nil {
		return false, fmt.Errorf("無法確認未建立的 Unicode 路徑是否衝突：%w", err)
	}
	defer func() {
		if cleanup := os.RemoveAll(probe); cleanup != nil && err == nil {
			same, err = false, fmt.Errorf("無法移除路徑檢查暫存目錄：%w", cleanup)
		}
	}()
	f, err := os.OpenFile(filepath.Join(probe, a), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, err
	}
	ai, statErr := f.Stat()
	closeErr := f.Close()
	if statErr != nil {
		return false, statErr
	}
	if closeErr != nil {
		return false, closeErr
	}
	bi, err := os.Stat(filepath.Join(probe, b))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return os.SameFile(ai, bi), nil
}

// 同時排除備份名稱與目的檔重疊，防止 commit 或 rollback 覆蓋另一份輸出。
func validateFileTargets(paths []string) error {
	for i, a := range paths {
		for _, b := range paths[:i] {
			for _, pair := range [][2]string{{a, b}, {a + ".bak", b}, {a, b + ".bak"}} {
				same, err := sameFilesystemPath(pair[0], pair[1])
				if err != nil {
					return err
				}
				if same {
					return fmt.Errorf("檔案路徑衝突：%s 與 %s", a, b)
				}
			}
		}
	}
	return nil
}
