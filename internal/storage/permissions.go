package storage

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// privateMode is the mode of every database file the store creates or tightens.
const privateMode fs.FileMode = 0o600

// owned reports whether this process's effective user owns info's file.
func owned(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}

// tighten removes group and world access from path when this process owns it.
// It never refuses: an operator can own the data directory (for example a
// TrueNAS dataset), so a path it cannot change gives a warning, not an error.
// A missing path is not a problem. Path-based chmod keeps SQLite's POSIX locks:
// closing another descriptor of an open database file would release them.
func tighten(path string, mode func(fs.FileMode) fs.FileMode) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	perm := info.Mode().Perm()
	if perm&0o077 == 0 {
		return "", nil
	}
	want := mode(perm)
	if !owned(info) {
		return fmt.Sprintf("%s has mode %04o and another user owns it; it was not changed; restrict it to the service user", path, perm), nil
	}
	if err = os.Chmod(path, want); err != nil {
		return fmt.Sprintf("%s has mode %04o; changing it to %04o failed: %v", path, perm, want, err), nil
	}
	return fmt.Sprintf("%s changed from mode %04o to %04o", path, perm, want), nil
}

func privateDir(perm fs.FileMode) fs.FileMode { return perm &^ 0o077 }
func privateFile(fs.FileMode) fs.FileMode     { return privateMode }

// createPrivate creates path with mode 0600 when it is absent, without
// truncating existing content.
func createPrivate(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateMode)
	if err != nil {
		return err
	}
	return f.Close()
}
