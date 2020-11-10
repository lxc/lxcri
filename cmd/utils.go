package main

import (
	"encoding/json"
	"os"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
)

func readBundleSpec(specFilePath string) (spec *specs.Spec, err error) {
	specFile, err := os.Open(specFilePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to open spec file '%s'", specFilePath)
	}
	defer specFile.Close()
	err = json.NewDecoder(specFile).Decode(&spec)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to decode spec file")
	}

	return spec, nil
}
