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

// tighten sets mode 0600 on a database file with group or world access when
// this process owns it. It never refuses: a file it cannot change gives a
// warning, not an error. A missing file is not a problem. Path-based chmod keeps
// SQLite's POSIX locks: closing another descriptor of an open database file
// would release them.
func tighten(path string) (string, error) {
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
	if !owned(info) {
		return fmt.Sprintf("%s has mode %04o and another user owns it; it was not changed; restrict it to the service user", path, perm), nil
	}
	if err = os.Chmod(path, privateMode); err != nil {
		return fmt.Sprintf("%s has mode %04o; changing it to %04o failed: %v", path, perm, privateMode, err), nil
	}
	return fmt.Sprintf("%s changed from mode %04o to %04o", path, perm, privateMode), nil
}

// exposedDir reports a data directory with group or world access. The store
// never changes the directory: an operator's dataset (for example on TrueNAS)
// can carry ACLs or settings that a chmod would disturb, and the database files
// in it are 0600.
func exposedDir(dir string) (string, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return fmt.Sprintf("data directory %s has mode %04o; it was not changed; restrict it to the service user", dir, perm), nil
	}
	return "", nil
}

// createPrivate creates path with mode 0600 when it is absent, without
// truncating existing content.
func createPrivate(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, privateMode)
	if err != nil {
		return err
	}
	return f.Close()
}
