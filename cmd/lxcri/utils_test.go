package main

import (
	"testing"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"
)

func TestParseSignal(t *testing.T) {
	sig := parseSignal("9")
	require.Equal(t, unix.SIGKILL, sig)

	sig = parseSignal("kill")
	require.Equal(t, unix.SIGKILL, sig)

	sig = parseSignal("sigkill")
	require.Equal(t, unix.SIGKILL, sig)

	sig = parseSignal("KILL")
	require.Equal(t, unix.SIGKILL, sig)

	sig = parseSignal("SIGKILL")
	require.Equal(t, unix.SIGKILL, sig)

	sig = parseSignal("SIGNOTEXIST")
	require.Equal(t, unix.Signal(0), sig)

	sig = parseSignal("66")
	require.Equal(t, unix.Signal(66), sig)
}
