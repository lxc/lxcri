package lxcri

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveMountDestination_absolute(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "golang.test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder1"), 0750)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder2"), 0750)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder3"), 0750)
	require.NoError(t, err)
	err = os.Symlink("/folder2", filepath.Join(tmpdir, "folder1", "f2"))
	require.NoError(t, err)
	err = os.Symlink("/folder3", filepath.Join(tmpdir, "folder2", "f3"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpdir, "folder3", "test.txt"), []byte("hello"), 0640)
	require.NoError(t, err)

	p, err := resolveMountDestination(tmpdir, "/folder1/f2/f3/test.txt")
	require.Equal(t, filepath.Join(tmpdir, "/folder3/test.txt"), p)
	require.NoError(t, err)

	p, err = resolveMountDestination(tmpdir, "/folder1/f2/xxxxx/fooo")
	require.Equal(t, filepath.Join(tmpdir, "/folder2/xxxxx/fooo"), p)
	require.Error(t, err, os.ErrExist)

	p, err = resolveMountDestination(tmpdir, "/folder1/f2/f3/hello.txt")
	require.Equal(t, filepath.Join(tmpdir, "/folder3/hello.txt"), p)
	require.Error(t, err, os.ErrExist)
}

func TestResolveMountDestination_relative(t *testing.T) {
	tmpdir, err := os.MkdirTemp("", "golang.test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpdir)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder1"), 0750)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder2"), 0750)
	require.NoError(t, err)
	err = os.MkdirAll(filepath.Join(tmpdir, "folder3"), 0750)
	require.NoError(t, err)
	err = os.Symlink("../folder2", filepath.Join(tmpdir, "folder1", "f2"))
	require.NoError(t, err)
	err = os.Symlink("../folder3", filepath.Join(tmpdir, "folder2", "f3"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(tmpdir, "folder3", "test.txt"), []byte("hello"), 0640)
	require.NoError(t, err)

	//err = os.Symlink("../../folder2", filepath.Join(tmpdir, "folder1", "f2"))
	//require.NoError(t, err)

	p, err := resolveMountDestination(tmpdir, "/folder1/f2/f3/test.txt")
	require.Equal(t, filepath.Join(tmpdir, "/folder3/test.txt"), p)
	require.NoError(t, err)

	p, err = resolveMountDestination(tmpdir, "/folder1/f2/xxxxx/fooo")
	require.Equal(t, filepath.Join(tmpdir, "/folder2/xxxxx/fooo"), p)
	require.Error(t, err, os.ErrExist)

	p, err = resolveMountDestination(tmpdir, "/folder1/f2/f3/hello.txt")
	require.Equal(t, filepath.Join(tmpdir, "/folder3/hello.txt"), p)
	require.Error(t, err, os.ErrExist)
}

func TestFilterMountOptions(t *testing.T) {
	opts := strings.Split("rw,rprivate,noexec,nosuid,nodev,tmpcopyup,create=dir", ",")

	out := filterMountOptions(DefaultRuntime, "tmpfs", opts)
	require.Equal(t, []string{"rw", "noexec", "nosuid", "nodev", "create=dir"}, out)

	out = filterMountOptions(DefaultRuntime, "nosuchfs", opts)
	require.Equal(t, opts, out)
}
