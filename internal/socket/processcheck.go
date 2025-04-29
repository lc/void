package socket

import (
	"strings"

	"github.com/mitchellh/go-ps"
)

var _ ProcessChecker = (*DefaultProcessChecker)(nil)

// ProcessChecker is an interface for checking if a process is running.
type ProcessChecker interface {
	IsRunning(name string) bool
}

// DefaultProcessChecker provides the default implementation of ProcessChecker.
type DefaultProcessChecker struct{}

// IsRunning checks if a process with the given name is running.
func (pc *DefaultProcessChecker) IsRunning(name string) bool {
	procs, err := ps.Processes()
	if err != nil {
		return false
	}

	for _, proc := range procs {
		if procName := proc.Executable(); len(procName) >= len(name) {
			if strings.EqualFold(procName[:len(name)], name) {
				return true
			}
		}
	}
	return false
}
