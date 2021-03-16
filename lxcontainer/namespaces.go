package lxcontainer

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
)

type Namespace struct {
	Name      string
	CloneFlag int
}

var CgroupNamespace = Namespace{"cgroup", unix.CLONE_NEWCGROUP}
var IPCNamespace = Namespace{"ipc", unix.CLONE_NEWIPC}
var MountNamespace = Namespace{"mnt", unix.CLONE_NEWNS}
var NetworkNamespace = Namespace{"net", unix.CLONE_NEWNET}
var PIDNamespace = Namespace{"pid", unix.CLONE_NEWPID}
var TimeNamespace = Namespace{"time", unix.CLONE_NEWTIME}
var UserNamespace = Namespace{"user", unix.CLONE_NEWUSER}
var UTSNamespace = Namespace{"uts", unix.CLONE_NEWUTS}

// maps from CRIO namespace names to LXC names and clone flags
var namespaceMap = map[specs.LinuxNamespaceType]Namespace{
	specs.CgroupNamespace:  CgroupNamespace,
	specs.IPCNamespace:     IPCNamespace,
	specs.MountNamespace:   MountNamespace,
	specs.NetworkNamespace: NetworkNamespace,
	specs.PIDNamespace:     PIDNamespace,
	// specs.TimeNamespace:     TimeNamespace,
	specs.UserNamespace: UserNamespace,
	specs.UTSNamespace:  UTSNamespace,
}

func cloneFlags(namespaces []specs.LinuxNamespace) (int, error) {
	flags := 0
	for _, ns := range namespaces {
		n, exist := namespaceMap[ns.Type]
		if !exist {
			return 0, fmt.Errorf("namespace %s is not supported", ns.Type)
		}
		flags |= n.CloneFlag
	}
	return flags, nil
}

func configureNamespaces(clxc *Runtime, namespaces []specs.LinuxNamespace) error {
	seenNamespaceTypes := map[specs.LinuxNamespaceType]bool{}
	for _, ns := range namespaces {
		if _, ok := seenNamespaceTypes[ns.Type]; ok {
			return fmt.Errorf("duplicate namespace type %s", ns.Type)
		}
		seenNamespaceTypes[ns.Type] = true
		if ns.Path == "" {
			continue
		}

		n, supported := namespaceMap[ns.Type]
		if !supported {
			return fmt.Errorf("unsupported namespace %s", ns.Type)
		}
		configKey := fmt.Sprintf("lxc.namespace.share.%s", n.Name)
		if err := clxc.setConfigItem(configKey, ns.Path); err != nil {
			return err
		}
	}

	// from `man lxc.container.conf` - user and network namespace must be inherited together
	if !seenNamespaceTypes[specs.NetworkNamespace] && seenNamespaceTypes[specs.UserNamespace] {
		return fmt.Errorf("to inherit the network namespace the user namespace must be inherited as well")
	}

	nsToKeep := make([]string, 0, len(namespaceMap))
	for key, n := range namespaceMap {
		if !seenNamespaceTypes[key] {
			nsToKeep = append(nsToKeep, n.Name)
		}
	}
	return clxc.setConfigItem("lxc.namespace.keep", strings.Join(nsToKeep, " "))
}

func isNamespaceEnabled(spec *specs.Spec, nsType specs.LinuxNamespaceType) bool {
	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == nsType {
			return true
		}
	}
	return false
}

func getNamespace(nsType specs.LinuxNamespaceType, namespaces []specs.LinuxNamespace) *specs.LinuxNamespace {
	for _, n := range namespaces {
		if n.Type == nsType {
			return &n
		}
	}
	return nil
}

// lxc does not set the hostname on shared namespaces
func setHostname(nsPath string, hostname string) error {
	// setns only affects the current thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	f, err := os.Open(nsPath)
	if err != nil {
		return fmt.Errorf("failed to open container uts namespace %q: %w", nsPath, err)
	}
	// #nosec
	defer f.Close()

	self, err := os.Open("/proc/self/ns/uts")
	if err != nil {
		return fmt.Errorf("failed to open uts namespace : %w", err)
	}
	// #nosec
	defer func() {
		unix.Setns(int(self.Fd()), unix.CLONE_NEWUTS)
		self.Close()
	}()

	err = unix.Setns(int(f.Fd()), unix.CLONE_NEWUTS)
	if err != nil {
		return fmt.Errorf("failed to switch to UTS namespace %s: %w", nsPath, err)
	}
	err = unix.Sethostname([]byte(hostname))
	if err != nil {
		return fmt.Errorf("unix.Sethostname failed: %w", err)
	}
	return nil
}
