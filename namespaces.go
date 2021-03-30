package lxcri

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// namespace is a mapping from the namespace name
// as used in /proc/{pid}/ns and the namespace clone flag,
// as defined in `man 2 clone`.
type namespace struct {
	Name      string
	CloneFlag int
}

var (
	cgroupNamespace  = namespace{"cgroup", unix.CLONE_NEWCGROUP}
	ipcNamespace     = namespace{"ipc", unix.CLONE_NEWIPC}
	mountNamespace   = namespace{"mnt", unix.CLONE_NEWNS}
	networkNamespace = namespace{"net", unix.CLONE_NEWNET}
	pidNamespace     = namespace{"pid", unix.CLONE_NEWPID}
	timeNamespace    = namespace{"time", unix.CLONE_NEWTIME}
	userNamespace    = namespace{"user", unix.CLONE_NEWUSER}
	utsNamespace     = namespace{"uts", unix.CLONE_NEWUTS}

	namespaceMap = map[specs.LinuxNamespaceType]namespace{
		specs.CgroupNamespace:  cgroupNamespace,
		specs.IPCNamespace:     ipcNamespace,
		specs.MountNamespace:   mountNamespace,
		specs.NetworkNamespace: networkNamespace,
		specs.PIDNamespace:     pidNamespace,
		// specs.timeNamespace:     timeNamespace,
		specs.UserNamespace: userNamespace,
		specs.UTSNamespace:  utsNamespace,
	}
)

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

func configureNamespaces(c *Container) error {
	seenNamespaceTypes := map[specs.LinuxNamespaceType]bool{}
	cloneNamespaces := make([]string, 0, len(c.Linux.Namespaces))

	for _, ns := range c.Linux.Namespaces {
		if _, seen := seenNamespaceTypes[ns.Type]; seen {
			return fmt.Errorf("duplicate namespace %s", ns.Type)
		}
		seenNamespaceTypes[ns.Type] = true

		n, supported := namespaceMap[ns.Type]
		if !supported {
			return fmt.Errorf("unsupported namespace %s", ns.Type)
		}

		if ns.Path == "" {
			cloneNamespaces = append(cloneNamespaces, n.Name)
			continue
		}

		configKey := fmt.Sprintf("lxc.namespace.share.%s", n.Name)
		if err := c.SetConfigItem(configKey, ns.Path); err != nil {
			return err
		}
	}

	return c.SetConfigItem("lxc.namespace.clone", strings.Join(cloneNamespaces, " "))
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
