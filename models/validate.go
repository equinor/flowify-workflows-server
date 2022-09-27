package models

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"

	_ "github.com/santhosh-tekuri/jsonschema/v5/httploader"
)

func findFileSchema(path string) (*jsonschema.Schema, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logrus.Debugf("no file path for %s", path)
		return nil, err
	}
	schema, err := jsonschema.Compile(path)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("found schema in file %s", path)
	return schema, nil
}

func findPrecompiledSchema(name string) (*jsonschema.Schema, error) {
	for k, v := range RegisteredSchemas {
		log.Debugf("comparing: %s == %s", name, k.Name())
		if k.Name() == name {
			logrus.Debugf("found registered schema for type %s and name %s", k, name)
			return v, nil
		}
	}
	logrus.Debugf("found no registered schemas for name %s", name)
	return nil, nil
}

func FindSchema(nameOrPath string) *jsonschema.Schema {

	fileSchema, _ := findFileSchema(nameOrPath)
	precompiledSchema, _ := findPrecompiledSchema(nameOrPath)
	if fileSchema != nil {
		logrus.Infof("using schema from file: %s", nameOrPath)
		return fileSchema
	}
	if precompiledSchema != nil {
		logrus.Infof("using precompiled schema for type: %s", nameOrPath)
		return precompiledSchema
	}
	logrus.Debugf("found no schemas for %s", nameOrPath)
	return nil
}

func Validate(data []byte, schemaFilePath string) error {

	schema := FindSchema(schemaFilePath)
	if schema == nil {
		return fmt.Errorf("could not find schema from: %s", schemaFilePath)
	}

	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return errors.Wrap(err, "validate unmarshal")
	}

	return schema.Validate(v)
}
