package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func parseSignal(sig string) unix.Signal {
	if sig == "" {
		return unix.SIGTERM
	}
	// handle numerical signal value
	if num, err := strconv.Atoi(sig); err == nil {
		return unix.Signal(num)
	}

	// gracefully handle all string variants e.g 'sigkill|SIGKILL|kill|KILL'
	s := strings.ToUpper(sig)
	if !strings.HasPrefix(s, "SIG") {
		s = "SIG" + s
	}
	return unix.SignalNum(s)
}

// createPidFile atomically creates a pid file for the given pid at the given path
func createPidFile(path string, pid int) error {
	tmpDir := filepath.Dir(path)
	tmpName := filepath.Join(tmpDir, fmt.Sprintf(".%s", filepath.Base(path)))

	// #nosec
	f, err := os.OpenFile(tmpName, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0600)
	if err != nil {
		return fmt.Errorf("failed to create temporary PID file %q: %w", tmpName, err)
	}
	_, err = fmt.Fprintf(f, "%d", pid)
	if err != nil {
		return fmt.Errorf("failed to write to temporary PID file %q: %w", tmpName, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close temporary PID file %q: %w", tmpName, err)
	}
	err = os.Rename(tmpName, path)
	if err != nil {
		return fmt.Errorf("failed to rename temporary PID file %q to %q: %w", tmpName, path, err)
	}
	return nil
}
