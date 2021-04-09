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
		return errorf("failed to decode JSON from %s: %w", src, err)
	}
	err = f.Close()
	if err != nil {
		return errorf("failed to close %s: %w", src, err)
	}
	return nil
}

func encodeFileJSON(dst string, obj interface{}, flags int, mode os.FileMode) error {
	f, err := os.OpenFile(dst, flags, mode)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	err = enc.Encode(obj)
	if err != nil {
		f.Close()
		return errorf("failed to encode JSON to %s: %w", dst, err)
	}
	err = f.Close()
	if err != nil {
		return errorf("failed to close %s: %w", dst, err)
	}
	// Use chmod because initial mode is affected by umask and flags.
	err = os.Chmod(dst, mode)
	if err != nil {
		return errorf("failed to 'chmod %o %s': %w", mode, dst, err)
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
