package models

import (
	"encoding/json"

	"github.com/pkg/errors"
)

type Value struct {
	Value  interface{} `json:"value" bson:"value"`
	Target string      `json:"target" bson:"target"`
}

func (v *Value) UnmarshalJSON(document []byte) error {
	var partialValue struct {
		Target string          `json:"target"`
		Value  json.RawMessage `json:"value"`
	}

	err := json.Unmarshal(document, &partialValue)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial value")
	}
	v.Target = partialValue.Target

	var arr []string
	err = json.Unmarshal(partialValue.Value, &arr)
	if err == nil {
		v.Value = arr
		return nil
	}

	var str string
	err = json.Unmarshal(partialValue.Value, &str)
	if err == nil {
		v.Value = str
		return nil
	}

	return err
}

type Job struct {
	Metadata `json:",inline" bson:",inline"`
	// the workflow is either a workflow or a reference to one in the database
	Type        ComponentType `json:"type" bson:"type"`
	InputValues []Value       `json:"inputValues,omitempty" bson:"inputValues,omitempty"`
	Workflow    Workflow      `json:"workflow" bson:"workflow"`
}

type JobPostRequest struct {
	Job           Job            `json:"job"`
	SubmitOptions JobPostOptions `json:"options"`
}

type JobPostOptions struct {
	Constants []interface{} `json:"constants"`
	Tags      []string      `json:"tags"`
}
