package internal

import (
	"encoding/json"
	"github.com/opencontainers/runtime-spec/specs-go"
	"os"
)

const (
	// CFG_DIR is bind mounted (readonly) to container
	CFG_DIR           = "/.crio-lxc"
	SYNC_FIFO_PATH    = CFG_DIR + "/syncfifo"
	SYNC_FIFO_CONTENT = "meshuggah rocks"
	INIT_CMD          = CFG_DIR + "/init"
	INIT_SPEC         = CFG_DIR + "/spec.json"
)

func ReadSpec(specFilePath string) (*specs.Spec, error) {
	specFile, err := os.Open(specFilePath)
	if err != nil {
		return nil, err
	}
	defer specFile.Close()
	spec := &specs.Spec{}
	err = json.NewDecoder(specFile).Decode(spec)
	if err != nil {
		return nil, err
	}
	return spec, nil
}

func WriteSpec(spec *specs.Spec, specFilePath string) error {
	f, err := os.OpenFile(specFilePath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0444)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(spec)
}
