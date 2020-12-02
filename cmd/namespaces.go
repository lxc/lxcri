package main

import (
	"fmt"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

type namespace struct {
	Name      string
	CloneFlag int
}

// maps from CRIO namespace names to LXC names and clone flags
var namespaceMap = map[specs.LinuxNamespaceType]namespace{
	specs.CgroupNamespace:  namespace{"cgroup", unix.CLONE_NEWCGROUP},
	specs.IPCNamespace:     namespace{"ipc", unix.CLONE_NEWIPC},
	specs.MountNamespace:   namespace{"mnt", unix.CLONE_NEWNS},
	specs.NetworkNamespace: namespace{"net", unix.CLONE_NEWNET},
	specs.PIDNamespace:     namespace{"pid", unix.CLONE_NEWPID},
	specs.UserNamespace:    namespace{"user", unix.CLONE_NEWUSER},
	specs.UTSNamespace:     namespace{"uts", unix.CLONE_NEWUTS},
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

func configureNamespaces(namespaces []specs.LinuxNamespace) error {
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
