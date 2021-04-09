package lxcri

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

// This file is only for helper functions that operate
// on (parts of) the runtime spec specs.Spec
// without any internal internal knowledge.

// unmapContainerID returns the (user/group) ID to which the given
// ID is mapped to by the given idmaps.
// The returned id will be equal to the given id
// if it is not mapped by the given idmaps.
func unmapContainerID(id uint32, idmaps []specs.LinuxIDMapping) uint32 {
	for _, idmap := range idmaps {
		if idmap.Size < 1 {
			continue
		}
		maxID := idmap.ContainerID + idmap.Size - 1
		// check if c.Process.UID is contained in the mapping
		if (id >= idmap.ContainerID) && (id <= maxID) {
			offset := id - idmap.ContainerID
			hostid := idmap.HostID + offset
			return hostid
		}
	}
	// uid is not mapped
	return id
}
