package mocks

import (
	"io/fs"
	"os"

	"github.com/stretchr/testify/mock"

	"github.com/lc/void/internal/filesys"
)

var (
	_ filesys.ReadWriteFS = (*MockOsFS)(nil)
	_ filesys.FileOps     = (*MockOsFS)(nil)
)

// MockOsFS is a mock implementation of the ReadWriteFS and FileOps interfaces.
// It is generated using testify/mock and adheres to the methods defined in the osFS struct.
type MockOsFS struct {
	mock.Mock
}

// Stat mocks the Stat method.
func (m *MockOsFS) Stat(p string) (fs.FileInfo, error) {
	args := m.Called(p)
	// Need to handle potential nil interface return
	var fileInfo fs.FileInfo
	if args.Get(0) != nil {
		fileInfo = args.Get(0).(fs.FileInfo)
	}
	return fileInfo, args.Error(1)
}

// MkdirAll mocks the MkdirAll method.
func (m *MockOsFS) MkdirAll(p string, mode os.FileMode) error {
	args := m.Called(p, mode)
	return args.Error(0)
}

// Open mocks the Open method.
func (m *MockOsFS) Open(p string) (*os.File, error) {
	args := m.Called(p)
	// Need to handle potential nil pointer return
	var file *os.File
	if args.Get(0) != nil {
		file = args.Get(0).(*os.File)
	}
	return file, args.Error(1)
}

// ReadFile mocks the ReadFile method.
func (m *MockOsFS) ReadFile(p string) ([]byte, error) {
	args := m.Called(p)
	// Need to handle potential nil slice return
	var data []byte
	if args.Get(0) != nil {
		data = args.Get(0).([]byte)
	}
	return data, args.Error(1)
}

// WriteFile mocks the WriteFile method.
func (m *MockOsFS) WriteFile(p string, b []byte, mode os.FileMode) error {
	args := m.Called(p, b, mode)
	return args.Error(0)
}

// CreateTemp mocks the CreateTemp method.
func (m *MockOsFS) CreateTemp(dir, pat string) (*os.File, error) {
	args := m.Called(dir, pat)
	// Need to handle potential nil pointer return
	var file *os.File
	if args.Get(0) != nil {
		file = args.Get(0).(*os.File)
	}
	return file, args.Error(1)
}

// Rename mocks the Rename method.
func (m *MockOsFS) Rename(old, newPath string) error {
	args := m.Called(old, newPath)
	return args.Error(0)
}

// Remove mocks the Remove method.
func (m *MockOsFS) Remove(p string) error {
	args := m.Called(p)
	return args.Error(0)
}

// Chmod mocks the Chmod method.
func (m *MockOsFS) Chmod(p string, mode os.FileMode) error {
	args := m.Called(p, mode)
	return args.Error(0)
}
