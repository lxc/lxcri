package lxcontainer

import (
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
	"testing"
)

func TestParseSystemCgroupPath(t *testing.T) {
	s := "kubepods-burstable-123.slice:crio:ABC"
	cg := parseSystemdCgroupPath(s)
	require.Equal(t, "kubepods.slice/kubepods-burstable.slice/kubepods-burstable-123.slice/crio-ABC.scope", cg)
}

func TestParseSignal(t *testing.T) {
	sig, err := parseSignal("9")
	require.NoError(t, err)
	require.Equal(t, unix.SIGKILL, sig)

	sig, err = parseSignal("kill")
	require.NoError(t, err)
	require.Equal(t, unix.SIGKILL, sig)

	sig, err = parseSignal("sigkill")
	require.NoError(t, err)
	require.Equal(t, unix.SIGKILL, sig)

	sig, err = parseSignal("KILL")
	require.NoError(t, err)
	require.Equal(t, unix.SIGKILL, sig)

	sig, err = parseSignal("SIGKILL")
	require.NoError(t, err)
	require.Equal(t, unix.SIGKILL, sig)

	sig, err = parseSignal("SIGNOTEXIST")
	require.Error(t, err)
	require.Equal(t, sigzero, sig)

	sig, err = parseSignal("66")
	require.Error(t, err)
	require.Equal(t, sigzero, sig)
}
