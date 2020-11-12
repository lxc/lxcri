package internal

import (
	"encoding/json"
	"github.com/opencontainers/runtime-spec/specs-go"
	"os"
)

const (
	// ConfigDir is the path to the crio-lxc resources relative to the container rootfs.
	ConfigDir = "/.crio-lxc"
	// SyncFifoPath is the path to the fifo used to block container start in init until start cmd is called.
	SyncFifoPath = ConfigDir + "/syncfifo"
	// SyncFifoContent is the content exchanged through the sync fifo.
	SyncFifoContent = "meshuggah rocks"
	// InitCmd is the path where the init binary is bind mounted.
	InitCmd = ConfigDir + "/init"
	// InitSpec is the path where the modified runtime spec is written to.
	// The init command loads the spec from this path.
	InitSpec = ConfigDir + "/spec.json"
)

// ReadSpec deserializes the JSON encoded runtime spec from the given path.
func ReadSpec(specFilePath string) (*specs.Spec, error) {
	// #nosec
	specFile, err := os.Open(specFilePath)
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

// WriteSpec serializes the runtime spec to JSON and writes it to the given path.
func WriteSpec(spec *specs.Spec, specFilePath string) error {
	// #nosec
	f, err := os.OpenFile(specFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0444)
	if err != nil {
		return err
	}
	// #nosec
	defer f.Close()
	if err := json.NewEncoder(f).Encode(spec); err != nil {
		return err
	}
	return f.Sync()
}
