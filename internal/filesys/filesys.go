// Package filesys provides file system abstractions and utilities for the Void application.
// It defines interfaces for file operations and provides implementations that
// delegate to the standard library, making it easier to test code that interacts
// with the file system.
package filesys

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// ReadWriteFS is the tiny surface the *config loader* needs.
// It is intentionally **smaller** than os.File because callers
// never need random-access writes or directory iteration.
type ReadWriteFS interface {
	Stat(string) (fs.FileInfo, error)
	MkdirAll(string, os.FileMode) error
	Open(string) (*os.File, error)
	WriteFile(string, []byte, os.FileMode) error
}

// FileOps is what the *PF manager* needs for its atomicWrite helper.
type FileOps interface {
	Open(string) (*os.File, error)
	ReadFile(string) ([]byte, error)
	MkdirAll(string, os.FileMode) error
	CreateTemp(string, string) (*os.File, error)
	Rename(string, string) error
	Remove(string) error
	Chmod(string, os.FileMode) error
}

// OS returns a file system implementation that delegates to the standard library.
// The returned implementation satisfies both ReadWriteFS and FileOps interfaces.
func OS() OsFS {
	return OsFS{}
}

// OsFS implements both ReadWriteFS and FileOps against the local disk.
// All methods delegate to the standard library.
type OsFS struct{}

func (OsFS) Stat(p string) (fs.FileInfo, error)     { return os.Stat(p) }
func (OsFS) MkdirAll(p string, m os.FileMode) error { return os.MkdirAll(p, m) }
func (OsFS) Open(p string) (*os.File, error)        { return os.Open(p) }
func (OsFS) ReadFile(p string) ([]byte, error) {
	return os.ReadFile(p)
}
func (OsFS) WriteFile(p string, b []byte, m os.FileMode) error { return os.WriteFile(p, b, m) }
func (OsFS) CreateTemp(dir, pat string) (*os.File, error)      { return os.CreateTemp(dir, pat) }
func (OsFS) Rename(old, newName string) error                  { return os.Rename(old, newName) }
func (OsFS) Remove(p string) error                             { return os.Remove(p) }
func (OsFS) Chmod(p string, m os.FileMode) error               { return os.Chmod(p, m) }

var (
	_ ReadWriteFS = OsFS{}
	_ FileOps     = OsFS{}
)

// AtomicWrite atomically persists data to dst with the provided file mode.
// The write is crash-safe on local filesystems:
//
//  1. temp file in the same dir
//  2. fsync(temp) + close
//  3. chmod(temp, perm)  (so rename doesn’t carry 0600 default)
//  4. rename(temp, dst)
//  5. fsync(dir)
//
// Callers supply an injected FileOps implementation so the function
// remains unit-testable with an in-memory FS.
func AtomicWrite(fs FileOps, dst string, data []byte, perm fs.FileMode) error {
	dir := filepath.Dir(dst)
	tmp, err := fs.CreateTemp(dir, ".void-*")
	if err != nil {
		return err
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	cerr := tmp.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		if removeErr := fs.Remove(tmp.Name()); removeErr != nil {
			// Log the error but continue with the original error
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temp file %s: %v\n", tmp.Name(), removeErr)
		}
		return err
	}
	if err = fs.Chmod(tmp.Name(), perm); err != nil {
		if removeErr := fs.Remove(tmp.Name()); removeErr != nil {
			// Log the error but continue with the original error
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temp file %s: %v\n", tmp.Name(), removeErr)
		}
		return err
	}
	if err = fs.Rename(tmp.Name(), dst); err != nil {
		if removeErr := fs.Remove(tmp.Name()); removeErr != nil {
			// Log the error but continue with the original error
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temp file %s: %v\n", tmp.Name(), removeErr)
		}
		return err
	}
	if d, err2 := fs.Open(dir); err2 == nil {
		if syncErr := d.Sync(); syncErr != nil {
			// Log the error but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to sync directory %s: %v\n", dir, syncErr)
		}
		if closeErr := d.Close(); closeErr != nil {
			// Log the error but continue
			fmt.Fprintf(os.Stderr, "Warning: failed to close directory %s: %v\n", dir, closeErr)
		}
	}
	return nil
}
