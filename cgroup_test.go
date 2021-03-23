package lxcri

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseSystemCgroupPath(t *testing.T) {
	s := "kubepods-burstable-123.slice:crio:ABC"
	cg := parseSystemdCgroupPath(s)
	require.Equal(t, "kubepods.slice/kubepods-burstable.slice/kubepods-burstable-123.slice/crio-ABC.scope", cg)
}
