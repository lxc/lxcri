package main

import (
	"encoding/json"
	"github.com/pkg/errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// ContainerInfo holds the information about a single container.
// It is created at 'create' within the container runtime dir and not changed afterwards.
// It is removed when the container is deleted.
type ContainerInfo struct {
	ContainerID string
	CreatedAt   time.Time
	RuntimeRoot string

	BundlePath    string
	ConsoleSocket string `json;,omitempty`
	// PidFile is the absolute path to the PID file of the container monitor process (crio-lxc-start)
	PidFile          string
	MonitorCgroupDir string

	// values derived from spec
	CgroupDir string

	// feature gates
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool
}

func (c ContainerInfo) SpecPath() string {
	return filepath.Join(c.BundlePath, "config.json")
}

// RuntimePath returns the absolute path witin the container root
func (c ContainerInfo) RuntimePath(subPath ...string) string {
	return filepath.Join(c.RuntimeRoot, c.ContainerID, filepath.Join(subPath...))
}

func (c ContainerInfo) ConfigFilePath() string {
	return c.RuntimePath("config")
}

func (c ContainerInfo) Pid() (int, error) {
	// #nosec
	data, err := ioutil.ReadFile(c.PidFile)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	return strconv.Atoi(s)
}

func (c ContainerInfo) CreatePidFile(pid int) error {
	return createPidFile(c.PidFile, pid)
}

// Spec deserializes the JSON encoded runtime spec from the given path.
func (c ContainerInfo) Spec() (*specs.Spec, error) {
	// #nosec
	specFile, err := os.Open(c.SpecPath())
	if err != nil {
		return nil, err
	}
	// #nosec
	defer specFile.Close()
	spec := &specs.Spec{}
	err = json.NewDecoder(specFile).Decode(spec)
	if err != nil {
		return nil, err
	}

	return spec, nil
}

func (c *ContainerInfo) Load() error {
	p := c.RuntimePath("container.json")
	data, err := ioutil.ReadFile(p)
	if err != nil {
		return errors.Wrapf(err, "failed to read bundle config file %s", p)
	}
	err = json.Unmarshal(data, c)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshal bundle config")
	}
	return nil
}

func (c *ContainerInfo) Create() error {
	p := c.RuntimePath("container.json")
	f, err := os.OpenFile(p, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return errors.Wrapf(err, "failed to create bundle config file %s", p)
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	err = enc.Encode(c)
	if err != nil {
		f.Close()
		return errors.Wrap(err, "failed to marshal bundle config")
	}
	return f.Close()
}
