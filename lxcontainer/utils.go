package lxcontainer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"golang.org/x/sys/unix"
)

// createPidFile atomically creates a pid file for the given pid at the given path
func CreatePidFile(path string, pid int) error {
	tmpDir := filepath.Dir(path)
	tmpName := filepath.Join(tmpDir, fmt.Sprintf(".%s", filepath.Base(path)))

	// #nosec
	f, err := os.OpenFile(tmpName, os.O_RDWR|os.O_CREATE|os.O_EXCL|os.O_SYNC, 0600)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%d", pid)
	if err != nil {
		return err
	}
	err = f.Close()
	if err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func canExecute(cmds ...string) error {
	for _, c := range cmds {
		if err := unix.Access(c, unix.X_OK); err != nil {
			return fmt.Errorf("can not execute %q: %w", c, err)
		}
	}
	return nil
}

func fsMagic(fsName string) int64 {
	switch fsName {
	case "proc", "procfs":
		return unix.PROC_SUPER_MAGIC
	case "cgroup2", "cgroup2fs":
		return unix.CGROUP2_SUPER_MAGIC
	default:
		return -1
	}
}

// TODO check whether dir is the filsystem root (use /proc/mounts)
func isFilesystem(dir string, fsName string) error {
	fsType := fsMagic(fsName)
	if fsType == -1 {
		return fmt.Errorf("undefined filesystem %q", fsName)
	}

	var stat unix.Statfs_t
	err := unix.Statfs(dir, &stat)
	if err != nil {
		return fmt.Errorf("fstat failed for %q: %w", dir, err)
	}
	if stat.Type != fsType {
		return fmt.Errorf("%q is not on filesystem %s", dir, fsName)
	}
	return nil
}

func decodeFileJSON(obj interface{}, src string) error {
	// #nosec
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	// #nosec
	err = json.NewDecoder(f).Decode(obj)
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to decode JSON from %s: %w", src, err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", src, err)
	}
	return nil
}

func encodeFileJSON(dst string, obj interface{}, flags int, mode uint32) error {
	f, err := os.OpenFile(dst, flags, os.FileMode(mode))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	//enc.SetIndent("", "  ")
	err = enc.Encode(obj)
	if err != nil {
		f.Close()
		return fmt.Errorf("failed to encode JSON to %s: %w", dst, err)
	}
	if err != nil {
		return fmt.Errorf("failed to close %s: %w", dst, err)
	}
	return nil
}

func nullTerminatedString(data []byte) string {
	i := bytes.Index(data, []byte{0})
	return string(data[:i])
}

func errorf(sfmt string, args ...interface{}) error {
	_, file, line, ok := runtime.Caller(1)
	if ok {
		return fmt.Errorf(filepath.Base(file)+":"+strconv.Itoa(line)+" "+sfmt, args...)
	}
	return fmt.Errorf(sfmt, args...)
}
