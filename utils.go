package lxcri

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// createPidFile atomically creates a pid file for the given pid at the given path
func CreatePidFile(path string, pid int) error {
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
	bin := filepath.Base(os.Args[0])
	_, file, line, _ := runtime.Caller(1)
	prefix := fmt.Sprintf("[%s:%s:%d] ", bin, filepath.Base(file), line)
	return fmt.Errorf(prefix+sfmt, args...)
}

// Modified version of golang standard library os.MkdirAll
func mkdirAll(path string, perm os.FileMode, uid int, gid int) error {
	// Fast path: if we can tell whether path is a directory or file, stop with success or error.
	dir, err := os.Stat(path)
	if err == nil {
		if dir.IsDir() {
			return nil
		}
		return &os.PathError{Op: "mkdir", Path: path, Err: unix.ENOTDIR}
	}

	// Slow path: make sure parent exists and then call Mkdir for path.
	i := len(path)
	for i > 0 && os.IsPathSeparator(path[i-1]) { // Skip trailing path separator.
		i--
	}

	j := i
	for j > 0 && !os.IsPathSeparator(path[j-1]) { // Scan backward over element.
		j--
	}

	if j > 1 {
		// Create parent.
		err = mkdirAll(path[:j-1], perm, uid, gid)
		if err != nil {
			return err
		}
	}

	// Parent now exists; invoke Mkdir and use its result.
	err = os.Mkdir(path, perm)
	if err != nil {
		// Handle arguments like "foo/." by
		// double-checking that directory doesn't exist.
		dir, err1 := os.Lstat(path)
		if err1 == nil && dir.IsDir() {
			return nil
		}
		return err
	}
	return unix.Chown(path, uid, gid)
}

func setenv(env []string, key, val string, overwrite bool) []string {
	for i, kv := range env {
		if strings.HasPrefix(kv, key+"=") {
			if overwrite {
				env[i] = key + "=" + val
			}
			return env
		}
	}
	return append(env, key+"="+val)
}
