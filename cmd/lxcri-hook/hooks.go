package main

import (
	"errors"
	"os"
	"strings"
)

type HookType string

const (
	HookPreStart  HookType = "pre-start"
	HookPreMount           = "pre-mount"
	HookMount              = "mount"
	HookAutodev            = "autodev"
	HookStartHost          = "start-host"
	HookStart              = "start"
	HookStop               = "stop"
	HookPostStop           = "post-stop"
	HookClone              = "clone"
	HookDestroy            = "destroy"
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

var ErrEnv = errors.New("LXC_HOOK_TYPE environment variable is not set")

// Env parses all environment variables available in liblxc hooks,
// and returns the parsed values in an Env struct.
// Env only parses the environment if`LXC_HOOK_TYPE` is set,
// and will return nil otherwise.
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
