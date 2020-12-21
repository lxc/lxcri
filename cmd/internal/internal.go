package internal

import (
	"encoding/json"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"os"
	"time"
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

// WriteFifo writes to the SyncFifo to synchronize container process init 
func WriteFifo() error {
	f, err := os.OpenFile(SyncFifoPath, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.Write([]byte(SyncFifoContent))
	if err != nil {
		return err
	}
	return f.Close()
}

// ReadFifo reads the content from the SyncFifo that was written by #WriteFifo.
// The read operation is aborted after the given timeout.
func ReadFifo(fifoPath string, timeout time.Duration) error {
	// #nosec
	f, err := os.OpenFile(fifoPath, os.O_RDONLY, 0)
	if err != nil {
		return errors.Wrap(err, "failed to open sync fifo")
	}
	err = f.SetDeadline(time.Now().Add(timeout))
	if err != nil {
		return errors.Wrap(err, "failed to set deadline")
	}
	// #nosec
	defer f.Close()

	data := make([]byte, len(SyncFifoContent))
	n, err := f.Read(data)
	if err != nil {
		return errors.Wrap(err, "problem reading from fifo")
	}
	if n != len(SyncFifoContent) || string(data) != SyncFifoContent {
		return errors.Errorf("bad fifo content: %s", string(data))
	}
	return nil
}
