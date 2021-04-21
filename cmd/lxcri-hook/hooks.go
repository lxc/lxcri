package main

import (
	"errors"
	"os"
	"strings"
)

// HookType is the liblxc hook type.
type HookType string

// List of liblxc hook types.
const (
	HookPreStart  HookType = "pre-start"
	HookPreMount  HookType = "pre-mount"
	HookMount     HookType = "mount"
	HookAutodev   HookType = "autodev"
	HookStartHost HookType = "start-host"
	HookStart     HookType = "start"
	HookStop      HookType = "stop"
	HookPostStop  HookType = "post-stop"
	HookClone     HookType = "clone"
	HookDestroy   HookType = "destroy"
	//HookPostStart          = "post-start" // not defined by liblxc
)

// Env is the parsed liblxc hook environment.
type Env struct {
	// CgroupAware is true if the container is cgroup namespace aware.
	CgroupAware bool
	// ConfigFile is the path to the container configuration file.
	ConfigFile string
	// Type is the hook type.
	Type HookType
	// Section is the hooks section type (e.g. 'lxc', 'net').
	Section string
	// Version is the version of the hooks
	Version string
	// LogLevel is the container's log level.
	LogLevel string
	// ContainerName is the container's name.
	ContainerName string
	// SharedNamespaces maps namespace names from /proc/{pid}/ns
	// to the file descriptor path referring to the container's namespace.
	SharedNamespaces map[string]string
	// RootfsMount is the path to the mounted root filesystem.
	RootfsMount string
	// RootfsPath is the lxc.rootfs.path entry for the container.
	RootfsPath string
	// SrcContainerName is the original container's name,
	// in the case of the clone hook.
	SrcContainerName string
}

var namespaces = []string{"cgroup", "ipc", "mnt", "net", "pid", "time", "user", "uts"}

// ErrEnv is the error returned by LoadEnv
// if the LXC_HOOK_TYPE environment variable is not set.
var ErrEnv = errors.New("LXC_HOOK_TYPE environment variable is not set")

// LoadEnv parses all liblxc hook environment variables,
// and returns the parsed values in an Env struct.
// If `LXC_HOOK_TYPE` is not set ErrEnv will be returned.
// NOTE The environment variables in liblxc hooks are all prefixed with LXC_.
func LoadEnv() (*Env, error) {
	hookType, exist := os.LookupEnv("LXC_HOOK_TYPE")
	if !exist {
		return nil, ErrEnv
	}

	env := &Env{
		ConfigFile:       os.Getenv("LXC_CONFIG_FILE"),
		Type:             HookType(hookType),
		Section:          os.Getenv("LXC_HOOK_SECTION"),
		Version:          os.Getenv("LXC_HOOK_VERSION"),
		LogLevel:         os.Getenv("LXC_LOG_LEVEL"),
		ContainerName:    os.Getenv("LXC_NAME"),
		RootfsMount:      os.Getenv("LXC_ROOTFS_MOUNT"),
		RootfsPath:       os.Getenv("LXC_ROOTFS_PATH"),
		SrcContainerName: os.Getenv("LXC_SRC_NAME"),
	}

	env.SharedNamespaces = make(map[string]string, len(namespaces))
	for _, ns := range namespaces {
		if val, ok := os.LookupEnv("LXC_" + strings.ToUpper(ns) + "_NS"); ok {
			env.SharedNamespaces[ns] = val
		}
	}
	return env, nil
}
