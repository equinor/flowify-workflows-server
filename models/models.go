package models

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v5"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
	"go.mongodb.org/mongo-driver/x/bsonx/bsoncore"
	corev1 "k8s.io/api/core/v1"
)

var (
	//go:embed spec/*
	StaticSpec        embed.FS
	RegisteredSchemas map[reflect.Type]*jsonschema.Schema
)

const (
	K8scontainer    ImplementationType = "k8scontainer"
	BrickType       ComponentType      = `brick`
	AnyType         ComponentType      = `any`
	GraphType       ComponentType      = `graph`
	MapType         ComponentType      = `map`
	ConditionalType ComponentType      = `conditional`
	ArgSrcPort      string             = `port`
	ArgSrcFile      string             = `file`
	WorkflowType    string             = `workflow`

	FlowifyArtifactType       string = "artifact"
	FlowifyParameterType      string = "parameter"
	FlowifySecretType         string = "env_secret"
	FlowifyParameterArrayType string = "parameter_array"
	FlowifyVolumeType         string = "volume"

	VersionInit      VersionNumber = VersionNumber(1)
	VersionTagLatest string        = "latest"
)

func contains(slice []string, value string) bool {
	for _, a := range slice {
		if a == value {
			return true
		}
	}
	return false
}

type ComponentType string

type FlowifyVolume struct {
	Uid       ComponentReference `json:"uid"`
	Workspace string             `json:"workspace"`
	Volume    corev1.Volume      `json:"volume"`
}

type FlowifyVolumeList struct {
	Items    []FlowifyVolume `json:"items"`
	PageInfo PageInfo        `json:"pageInfo"`
}

func init() {
	// compile the schemas once, on startup
	compiler := jsonschema.NewCompiler()

	fs.WalkDir(StaticSpec, "spec", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".schema.json") {
			return nil
		}
		f, err := StaticSpec.Open(path)
		if err != nil {
			log.Panicf("unexpected static file content in (%s): %v", d.Name(), err)
			return err
		}
		compiler.AddResource(d.Name(), f)
		return nil
	})
	RegisteredSchemas = make(map[reflect.Type]*jsonschema.Schema, 10)

	schemas := []struct {
		Type     reflect.Type
		Filename string
	}{
		{Type: reflect.TypeOf(Component{}), Filename: "component.schema.json"},
		{Type: reflect.TypeOf(Workflow{}), Filename: "workflow.schema.json"},
		{Type: reflect.TypeOf(Job{}), Filename: "job.schema.json"},
		{Type: reflect.TypeOf(Node{}), Filename: "node.schema.json"},
		{Type: reflect.TypeOf(ComponentPostRequest{}), Filename: "componentpostrequest.schema.json"},
		{Type: reflect.TypeOf(WorkflowPostRequest{}), Filename: "workflowpostrequest.schema.json"},
		{Type: reflect.TypeOf(JobPostRequest{}), Filename: "jobpostrequest.schema.json"},
		{Type: reflect.TypeOf(MetadataList{}), Filename: "metadatalist.schema.json"},
		{Type: reflect.TypeOf(MetadataWorkspaceList{}), Filename: "metadataworkspacelist.schema.json"},
		{Type: reflect.TypeOf(FlowifyVolume{}), Filename: "volume.schema.json"},
		{Type: reflect.TypeOf(FlowifyVolumeList{}), Filename: "volumelist.schema.json"},
	}

	for _, s := range schemas {
		schema, err := compiler.Compile(s.Filename)
		if err != nil {
			log.Panicf("can't load json schema, %v", err)
		}
		RegisteredSchemas[s.Type] = schema
	}
}

var (
	ErrNoSchemaFound = fmt.Errorf("no schema found")
)

func ValidateDocument(document []byte, t reflect.Type) error {
	var v interface{}
	if err := json.Unmarshal(document, &v); err != nil {
		return errors.Wrapf(err, "type validate %s", t)
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	schema := RegisteredSchemas[t]
	if schema == nil {
		return ErrNoSchemaFound
	}

	if validation := schema.Validate(v); validation != nil {
		return errors.Wrapf(validation, "type validate %s", t)
	}
	return nil
}

type SourceTxt string

func (a SourceTxt) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bsontype.String, bsoncore.AppendString(nil, string(a)), nil
}

func (a *SourceTxt) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	if t != bsontype.String {
		return fmt.Errorf("invalid bson value type '%s'", t.String())
	}

	s, _, ok := bsoncore.ReadString(data)
	if !ok {
		return fmt.Errorf("invalid bson string value")
	}
	*a = SourceTxt(s)
	return nil
}

type ImplementationType string

type ComponentReference uuid.UUID

func NewComponentReference() ComponentReference {
	return ComponentReference(uuid.New())
}

func (r ComponentReference) String() string {
	return uuid.UUID(r).String()
}

func (id *ComponentReference) IsZero() bool {
	return *id == ComponentReference(uuid.Nil)
}

func (r ComponentReference) MarshalJSON() ([]byte, error) {
	txt, err := uuid.UUID(r).MarshalText()

	if err != nil {
		return nil, errors.Wrapf(err, "cannot marshal ComponentReference")
	}

	return []byte(fmt.Sprintf(`"%s"`, txt)), nil
}

func (r *ComponentReference) UnmarshalJSON(data []byte) error {
	obj, err := uuid.ParseBytes(data)

	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal ComponentReference from %s", string(data))
	}

	*r = ComponentReference(obj)
	return nil
}

func (c ComponentReference) MarshalBSONValue() (bsontype.Type, []byte, error) {
	return bsontype.String, bsoncore.AppendString(nil, c.String()), nil
}

func (c *ComponentReference) UnmarshalBSONValue(t bsontype.Type, data []byte) error {
	if t != bsontype.String {
		return fmt.Errorf("invalid bson value type '%s'", t.String())
	}
	s, _, ok := bsoncore.ReadString(data)
	if !ok {
		return fmt.Errorf("invalid bson string value")
	}
	obj, err := uuid.Parse(s)

	if err != nil {
		log.Debugf("input data len is %d", len(data))
		log.Debugf("input data is %s", string(data))
		return errors.Wrapf(err, "cannot unmarshal ComponentReference from %s", string(data))
	}

	*c = ComponentReference(obj)
	return nil
}

type ComponentPostRequest struct {
	Component Component            `json:"component"`
	Options   ComponentPostOptions `json:"options,omitempty"`
}

type WorkflowPostRequest struct {
	Workflow Workflow             `json:"workflow"`
	Options  ComponentPostOptions `json:"options,omitempty"`
}

type ComponentPostOptions struct {
	// TODO: Added for forward compatibility
}

// Uses MustParse. Only use for testing
func NewReference(s string) ComponentReference {
	return ComponentReference(uuid.MustParse(s))
}

type VersionNumber int

func (v VersionNumber) String() string {
	return strconv.Itoa(int(v))
}

type CRefVersion struct {
	Version VersionNumber      `json:"version,omitempty" bson:"version,omitempty"`
	Uid     ComponentReference `json:"uid,omitempty" bson:"uid,omitempty"`
}

func (c CRefVersion) String() string {
	return fmt.Sprintf("UID: %s, version: %d", c.Uid.String(), c.Version)
}

func (c CRefVersion) IsZero() bool {
	if c.Uid.IsZero() && c.Version == VersionNumber(0) {
		return true
	}
	return false
}

type Version struct {
	Current  VersionNumber `json:"current" bson:"current"`
	Tags     []string      `json:"tags,omitempty" bson:"tags,omitempty"`
	Previous CRefVersion   `json:"previous,omitempty" bson:"previous,omitempty"`
}

func (v *Version) SetTag(tag string) {
	if !contains(v.Tags, tag) {
		v.Tags = append(v.Tags, tag)
	}
}

func (v *Version) SetLatestTag() {
	v.SetTag(VersionTagLatest)
}

func (v *Version) InitializeNew() error {
	switch v.Current {
	case VersionInit:
		// do nothing
	case VersionNumber(0):
		v.Current = VersionInit
	default:
		return fmt.Errorf("cannot initialize new version with current version set to %s", v.Current.String())
	}
	v.SetLatestTag()
	return nil
}

type ModifiedBy struct {
	Oid   string `json:"oid" bson:"oid"`
	Email string `json:"email,omitempty" bson:"email,omitempty"`
}

type Metadata struct {
	/* ModifiedBy, Uid and Timestamp are client read-only */
	Name        string             `json:"name,omitempty" bson:"name,omitempty"`
	Description string             `json:"description,omitempty" bson:"description,omitempty"`
	ModifiedBy  ModifiedBy         `json:"modifiedBy,omitempty" bson:"modifiedBy,omitempty"`
	Uid         ComponentReference `json:"uid,omitempty" bson:"uid,omitempty"`
	Version     Version            `json:"version,omitempty" bson:"version,omitempty"`
	Timestamp   time.Time          `json:"timestamp" bson:"timestamp"`
}

type MetadataWorkspace struct {
	// Metadata with with Workspace
	Metadata  `json:",inline" bson:",inline"`
	Workspace string `json:"workspace" bson:"workspace"`
}

type Workflow struct {
	Metadata  `json:",inline" bson:",inline"`
	Component Component     `json:"component" bson:"component"`
	Type      ComponentType `json:"type" bson:"type"`
	Workspace string        `json:"workspace" bson:"workspace"`
}

type ComponentBase struct {
	Metadata `json:",inline" bson:",inline"`
	Inputs   []Data        `json:"inputs,omitempty"`
	Outputs  []Data        `json:"outputs,omitempty"`
	Type     ComponentType `json:"type"`
}

type Component struct {
	ComponentBase  `json:",inline" bson:",inline"`
	Implementation interface{} `json:"implementation"`
}

type ImplementationBase struct {
	Type ComponentType `json:"type" bson:"type"`
}

type Any struct {
	ImplementationBase `json:",inline" bson:",inline"`
}

type PortAddress struct {
	Node string `json:"node,omitempty" bson:"node,omitempty"`
	Port string `json:"port" bson:"port"`
}

type Edge struct {
	Source PortAddress `json:"source"`
	Target PortAddress `json:"target"`
}

type Expression struct {
	Left     interface{} `json:"left" bson:"left"`
	Right    interface{} `json:"right" bson:"right"`
	Operator string      `json:"operator" bson:"operator"`
}

func (e Expression) Validate(doc []byte) error {
	return ValidateDocument(doc, reflect.TypeOf(Expression{}))
}

func (e *Expression) UnmarshalJSON(document []byte) error {
	var partialExpression struct {
		Operator string          `json:"operator"`
		Left     json.RawMessage `json:"left"`
		Right    json.RawMessage `json:"right"`
	}

	err := json.Unmarshal(document, &partialExpression)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial expression")
	}
	e.Operator = partialExpression.Operator

	var strValue string
	var dataValue Data
	err = json.Unmarshal(partialExpression.Left, &strValue)
	if err == nil {
		e.Left = strValue
	} else {
		err = json.Unmarshal(partialExpression.Left, &dataValue)
		if err != nil {
			return errors.Wrap(err, "Cannot unmarshal expression (left value)")
		}
		e.Left = dataValue
	}

	err = json.Unmarshal(partialExpression.Right, &strValue)
	if err == nil {
		e.Right = strValue
	} else {
		err = json.Unmarshal(partialExpression.Right, &dataValue)
		if err != nil {
			return errors.Wrap(err, "Cannot unmarshal expression (right value)")
		}
		e.Right = dataValue
	}

	return nil
}

func (e *Expression) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal expression")
	}

	var partialExpression struct {
		Operator string   `bson:"operator"`
		Left     bson.Raw `bson:"left"`
		Right    bson.Raw `bson:"right"`
	}
	err = bson.Unmarshal(rawData, &partialExpression)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal expression")
	}
	e.Operator = partialExpression.Operator

	var strValue SourceTxt
	var dataValue Data
	err = strValue.UnmarshalBSONValue(bsontype.String, partialExpression.Left)
	if err == nil {
		e.Left = string(strValue)
	} else {
		err = bson.Unmarshal(partialExpression.Left, &dataValue)
		if err != nil {
			return errors.Wrapf(err, "Cannot unmarshal expression (left value)")
		}
		e.Left = dataValue
	}

	err = strValue.UnmarshalBSONValue(bsontype.String, partialExpression.Right)
	if err == nil {
		e.Right = string(strValue)
	} else {
		err = bson.Unmarshal(partialExpression.Right, &dataValue)
		if err != nil {
			return errors.Wrapf(err, "Cannot unmarshal expression (right value)")
		}
		e.Right = dataValue
	}

	return nil
}

type Node struct {
	Id   string      `json:"id" bson:"id"`
	Node interface{} `json:"node" bson:"node"`
	// the node has userdata which is never touched by the backend
	Userdata json.RawMessage `json:"userdata,omitempty" bson:"userdata,omitempty"`
}

func (n Node) Validate(doc []byte) error {
	return ValidateDocument(doc, reflect.TypeOf(Node{}))
}

// implements the json.Unmarshaler (cf. https://pkg.go.dev/encoding/json#Unmarshaler)
func (n *Node) UnmarshalJSON(document []byte) error {
	var partialNode struct {
		Id       string          `json:"id"`
		Node     json.RawMessage `json:"node"`
		Userdata json.RawMessage `json:"userdata"`
	}

	err := json.Unmarshal(document, &partialNode)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial node")
	}
	n.Id = partialNode.Id
	n.Userdata = partialNode.Userdata

	var cref ComponentReference
	err = json.Unmarshal(partialNode.Node, &cref)
	if err == nil {
		n.Node = cref
		return nil
	}

	var cmpInline Component
	err = json.Unmarshal(partialNode.Node, &cmpInline)
	if err == nil {
		n.Node = cmpInline
		return nil
	}

	var crefver CRefVersion
	err = json.Unmarshal(partialNode.Node, &crefver)
	if err == nil {
		n.Node = crefver
		return nil
	}

	return errors.Errorf("cannot unmarshal node, unrecognized type of node.Node, node Id: %s", partialNode.Id)
}

func (n *Node) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal node")
	}
	var partial struct {
		Id       string          `bson:"id"`
		Node     bson.Raw        `bson:"node"`
		Userdata json.RawMessage `bson:"userdata"`
	}

	err = bson.Unmarshal(rawData, &partial)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal partial node")
	}
	n.Id = partial.Id
	n.Userdata = partial.Userdata

	var cref ComponentReference
	// this is the tricky despatch point, needs to unmarshall as VALUE
	err = cref.UnmarshalBSONValue(bsontype.String, partial.Node)
	if err == nil {
		n.Node = cref
		return nil
	}

	var cmpInline Component
	err = bson.Unmarshal(partial.Node, &cmpInline)
	if err == nil {
		n.Node = cmpInline
		return nil
	}

	var crefver CRefVersion
	err = bson.Unmarshal(partial.Node, &crefver)
	if err == nil {
		n.Node = crefver
		return nil
	}

	return errors.Errorf("cannot unmarshal node, unrecognized type of node.Node, node Id: %s", partial.Id)
}

type Graph struct {
	ImplementationBase `json:",inline" bson:",inline"`
	Nodes              []Node `json:"nodes,omitempty" bson:"nodes"`
	Edges              []Edge `json:"edges,omitempty" bson:"edges"`
	InputMappings      []Edge `json:"inputMappings,omitempty" bson:"inputMappings,omitempty"`
	OutputMappings     []Edge `json:"outputMappings,omitempty" bson:"outputMappings,omitempty"`
}

// implements the json.Unmarshaler (cf. https://pkg.go.dev/encoding/json#Unmarshaler)
func (c *Component) UnmarshalJSON(document []byte) error {
	var partialComponent struct {
		ComponentBase  `json:",inline" bson:",inline"`
		Implementation json.RawMessage
	}

	err := json.Unmarshal(document, &partialComponent)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial component")
	}

	// assign all inherited fields
	c.ComponentBase = partialComponent.ComponentBase

	// inspect implementation sub-field "type" to find polymorphic type
	typeHolder := struct {
		Type ComponentType `json:"type"`
	}{}

	err = json.Unmarshal(partialComponent.Implementation, &typeHolder)
	if err != nil {
		fmt.Printf("cannot unmarshal 'type' from implementation object: %s\n", string(partialComponent.Implementation))
		return err
	}

	switch typeHolder.Type {
	case BrickType:
		var brick Brick
		err = json.Unmarshal(partialComponent.Implementation, &brick)
		if err != nil {
			fmt.Printf("Cannot unmarshal Brick from: %s\n", string(partialComponent.Implementation))
			return err
		}
		c.Implementation = brick
	case AnyType:
		var any Any
		err = json.Unmarshal(partialComponent.Implementation, &any)
		if err != nil {
			fmt.Printf("Cannot unmarshal any from: %s\n", string(partialComponent.Implementation))
			return err
		}
		c.Implementation = any
	case GraphType:
		var graph Graph
		err = json.Unmarshal(partialComponent.Implementation, &graph)
		if err != nil {
			fmt.Printf("Cannot unmarshal graph from: %s\n", string(partialComponent.Implementation))
			return err
		}
		c.Implementation = graph
	case MapType:
		var mapCmp Map
		err = json.Unmarshal(partialComponent.Implementation, &mapCmp)
		if err != nil {
			fmt.Printf("Cannot unmarshal map from: %s\n", string(partialComponent.Implementation))
			return err
		}
		c.Implementation = mapCmp
	case ConditionalType:
		var conditionalCmp Conditional
		err = json.Unmarshal(partialComponent.Implementation, &conditionalCmp)
		if err != nil {
			fmt.Printf("Cannot unmarshal conditional from: %s\n", string(partialComponent.Implementation))
			return err
		}
		c.Implementation = conditionalCmp
	default:
		return fmt.Errorf("no type json unmarshaling implemented for '%s'", typeHolder.Type)
	}
	return nil
}

/*
func (j *Job) UnmarshalJSON(document []byte) error {
	if err := TypeValidate(document, reflect.TypeOf(Job{})); err != nil {
		return errors.Wrap(err, "cannot unmarshal job")
	}

	type JobAlias Job // new type has no custom marshaler, so this wont recurse
	err := json.Unmarshal(document, (*JobAlias)(j))
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal job")
	}
	return nil
}


func (j *JobPostRequest) UnmarshalJSON(document []byte) error {
	if err := TypeValidate(document, reflect.TypeOf(JobPostRequest{})); err != nil {
		return errors.Wrap(err, "cannot unmarshal job")
	}

	type Alias JobPostRequest // new type has no custom marshaler, so this wont recurse
	err := json.Unmarshal(document, (*Alias)(j))
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal jobpostrequest")
	}
	return nil
}
*/

func (c *Component) UnmarshalBSON(data []byte) error {
	var partialComponent struct {
		ComponentBase  `json:",inline" bson:",inline"`
		Implementation ImplementationBase `json:"implementation" bson:"implementation"`
	}
	err := bson.Unmarshal(data, &partialComponent)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal component")
	}

	switch partialComponent.Implementation.Type {
	case AnyType:
		var tmpComponent struct {
			ComponentBase  `json:",inline" bson:",inline"`
			Implementation Any `json:"implementation" bson:"implementation"`
		}
		err := bson.Unmarshal(data, &tmpComponent)
		if err != nil {
			return errors.Wrapf(err, "cannot unmarshal brick component")
		}
		c.ComponentBase = tmpComponent.ComponentBase
		c.Implementation = tmpComponent.Implementation
	case BrickType:
		var tmpComponent struct {
			ComponentBase  `json:",inline" bson:",inline"`
			Implementation Brick `json:"implementation" bson:"implementation"`
		}
		err := bson.Unmarshal(data, &tmpComponent)
		if err != nil {
			return errors.Wrapf(err, "cannot unmarshal brick component")
		}
		c.ComponentBase = tmpComponent.ComponentBase
		c.Implementation = tmpComponent.Implementation
	case GraphType:
		var tmpComponent struct {
			ComponentBase  `json:",inline" bson:",inline"`
			Implementation Graph `json:"implementation" bson:"implementation"`
		}
		err := bson.Unmarshal(data, &tmpComponent)
		if err != nil {
			return errors.Wrapf(err, "cannot unmarshal graph component")
		}
		c.ComponentBase = tmpComponent.ComponentBase
		c.Implementation = tmpComponent.Implementation
	case MapType:
		var tmpComponent struct {
			ComponentBase  `json:",inline" bson:",inline"`
			Implementation Map `json:"implementation" bson:"implementation"`
		}
		err := bson.Unmarshal(data, &tmpComponent)
		if err != nil {
			return errors.Wrapf(err, "cannot unmarshal map component")
		}
		c.ComponentBase = tmpComponent.ComponentBase
		c.Implementation = tmpComponent.Implementation
	case ConditionalType:
		var tmpComponent struct {
			ComponentBase  `json:",inline" bson:",inline"`
			Implementation Conditional `json:"implementation" bson:"implementation"`
		}
		err := bson.Unmarshal(data, &tmpComponent)
		if err != nil {
			return errors.Wrapf(err, "cannot unmarshal conditional component")
		}
		c.ComponentBase = tmpComponent.ComponentBase
		c.Implementation = tmpComponent.Implementation
	default:
		return fmt.Errorf("no type bson unmarshaling implemented for '%s'", partialComponent.Implementation.Type)
	}

	c.ComponentBase = partialComponent.ComponentBase
	return nil
}

type PageInfo struct {
	TotalNumber int `json:"totalNumber"`
	Limit       int `json:"limit"`
	Skip        int `json:"skip"`
}

type MetadataList struct {
	Items []Metadata `json:"items,omitempty"`
	// Total number in query before pagination,
	PageInfo PageInfo `json:"pageInfo"`
}

type MetadataWorkspaceList struct {
	Items []MetadataWorkspace `json:"items,omitempty"`
	// Total number in query before pagination,
	PageInfo PageInfo `json:"pageInfo"`
}

type ArgumentTarget struct {
	Type   string `json:"type" bson:"type"`
	Prefix string `json:"prefix,omitempty" bson:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty" bson:"suffix,omitempty"`
}

type ArgumentBase struct {
	Description string         `json:"description,omitempty" bson:"description,omitempty"`
	Target      ArgumentTarget `json:"target,omitempty" bson:"target,omitempty"`
}

type Argument struct {
	ArgumentBase `json:",inline" bson:",inline"`
	Source       interface{} `json:"source" bson:"source"`
}

type ArgumentSourcePort struct {
	Port string `json:"port" bson:"port"`
}

type ArgumentSourceFile struct {
	File string `json:"file" bson:"file"`
}

func (a *Argument) UnmarshalJSON(document []byte) error {
	var partialArg struct {
		ArgumentBase `json:",inline"`
		Source       json.RawMessage `json:"source"`
	}

	err := json.Unmarshal(document, &partialArg)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial argument")
	}

	a.ArgumentBase = partialArg.ArgumentBase

	var txt string
	err = json.Unmarshal(partialArg.Source, &txt)
	if err == nil {
		a.Source = txt
		return nil
	}

	// var argSrcPort ArgumentSourcePort
	var argSrcPort struct {
		Port *string `json:"port"`
	}
	err = json.Unmarshal(partialArg.Source, &argSrcPort)
	if err == nil && argSrcPort.Port != nil {
		a.Source = ArgumentSourcePort{Port: *argSrcPort.Port}
		return nil
	}

	var argSrcFile struct {
		File *string `json:"file"`
	}
	err = json.Unmarshal(partialArg.Source, &argSrcFile)
	if err == nil && argSrcFile.File != nil {
		a.Source = ArgumentSourceFile{File: *argSrcFile.File}
		return nil
	}
	msg, _ := json.Marshal(partialArg.Source)
	return fmt.Errorf("no type json unmarshaling implemented for argument source '%s'", msg)
}

func (arg *Argument) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal argument")
	}

	var partial struct {
		ArgumentBase `bson:"inline"`
		Source       bson.Raw `bson:"source"`
	}
	err = bson.Unmarshal(rawData, &partial)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal argument")
	}

	arg.ArgumentBase = partial.ArgumentBase

	// check for string value
	var srcTxt SourceTxt
	err = srcTxt.UnmarshalBSONValue(bsontype.String, partial.Source)
	if err == nil {
		arg.Source = string(srcTxt)
		return nil
	}

	// check for port type
	w, err := partial.Source.LookupErr(ArgSrcPort)
	if err == nil {
		arg.Source = ArgumentSourcePort{Port: w.StringValue()}
		return nil
	}

	// check for file type
	w, err = partial.Source.LookupErr(ArgSrcFile)
	if err == nil {
		arg.Source = ArgumentSourceFile{File: w.StringValue()}
		return nil
	}

	return errors.Wrap(err, "cannot unmarshal argument, unknown source type")
}

type FileResultSource struct {
	File string `json:"file" bson:"file"`
	//
}

type VolumeResultSource struct {
	Volume string `json:"volume" bson:"volume"`
	//
}

type ResultBase struct {
	Description string      `json:"description,omitempty" bson:"description,omitempty"`
	Target      PortAddress `json:"target,omitempty" bson:"target"`
}

type Result struct {
	ResultBase `json:",inline" bson:",inline"`
	Source     interface{} `json:"source" bson:"source"`
}

// implements the json.Unmarshaler (cf. https://pkg.go.dev/encoding/json#Unmarshaler)
func (res *Result) UnmarshalJSON(document []byte) error {
	var partialResult struct {
		ResultBase `json:",inline"`
		Source     json.RawMessage `json:"source"`
	}

	err := json.Unmarshal(document, &partialResult)
	if err != nil {
		return errors.Wrap(err, "could not unmarshal partial result")
	}

	res.ResultBase = partialResult.ResultBase

	var str string
	err = json.Unmarshal(partialResult.Source, &str)
	if err == nil {
		// unmarshalled as string. everything is ok
		res.Source = str
		return nil
	}

	var volSource VolumeResultSource
	err = json.Unmarshal(partialResult.Source, &volSource)
	if err == nil && volSource.Volume != "" {
		// unmarshalled as volume. everything ok
		res.Source = volSource
		return nil
	}

	var fileSource FileResultSource
	err = json.Unmarshal(partialResult.Source, &fileSource)
	if err == nil {
		// unmarshalled as file ref. everything ok
		res.Source = fileSource
		return nil
	}

	return fmt.Errorf("could not unmarshal result source")
}

func (res *Result) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal result")
	}

	var partial struct {
		ResultBase `bson:"inline"`
		Source     bson.Raw `bson:"source"`
	}
	err = bson.Unmarshal(rawData, &partial)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal result")
	}

	res.ResultBase = partial.ResultBase

	// check for string value
	var srcTxt SourceTxt
	err = srcTxt.UnmarshalBSONValue(bsontype.String, partial.Source)
	if err == nil {
		res.Source = string(srcTxt)
		return nil
	}

	// check for port address
	var resSource FileResultSource
	err = bson.Unmarshal(partial.Source, &resSource)
	if err == nil {
		res.Source = resSource
		return nil
	}

	return errors.Wrap(err, "cannot unmarshal argument, unknown result type")
}

type Brick struct {
	ImplementationBase `json:",inline" bson:",inline"`
	Container          *corev1.Container `json:"container"`
	Args               []Argument        `json:"args,omitempty" bson:"args"`
	Results            []Result          `json:"results,omitempty" bson:"results"`
}

type Data struct {
	Name      string   `json:"name"`
	MediaType []string `json:"mediatype,omitempty"`
	Type      string   `json:"type"`
	// opaque userdata never touched by the backend
	Userdata json.RawMessage `json:"userdata,omitempty"`
}

type Map struct {
	ImplementationBase `json:",inline" bson:",inline"`
	Node               interface{} `json:"node" bson:"node"`
	InputMappings      []Edge      `json:"inputMappings,omitempty" bson:"inputMappings,omitempty"`
	OutputMappings     []Edge      `json:"outputMappings,omitempty" bson:"outputMappings,omitempty"`
}

func (m *Map) UnmarshalJSON(document []byte) error {
	var partialMapCmp struct {
		ImplementationBase `json:",inline"`
		Node               json.RawMessage `json:"node"`
		InputMappings      []Edge          `json:"inputMappings,omitempty"`
		OutputMappings     []Edge          `json:"outputMappings,omitempty"`
	}

	err := json.Unmarshal(document, &partialMapCmp)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial map component")
	}
	m.ImplementationBase = partialMapCmp.ImplementationBase
	m.InputMappings = partialMapCmp.InputMappings
	m.OutputMappings = partialMapCmp.OutputMappings

	var cref ComponentReference
	err = json.Unmarshal(partialMapCmp.Node, &cref)
	if err == nil {
		m.Node = cref
		return nil
	}

	var cmpInline Component
	err = json.Unmarshal(partialMapCmp.Node, &cmpInline)
	if err == nil {
		m.Node = cmpInline
		return nil
	}

	var crefver CRefVersion
	err = json.Unmarshal(partialMapCmp.Node, &crefver)
	if err == nil {
		m.Node = crefver
		return nil
	}

	return err
}

func (m *Map) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal map component")
	}
	var partialMapCmp struct {
		ImplementationBase `bson:",inline"`
		Node               bson.Raw `bson:"node"`
		InputMappings      []Edge   `bson:"inputMappings,omitempty"`
		OutputMappings     []Edge   `bson:"outputMappings,omitempty"`
	}

	err = bson.Unmarshal(rawData, &partialMapCmp)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal partial map component")
	}
	m.ImplementationBase = partialMapCmp.ImplementationBase
	m.InputMappings = partialMapCmp.InputMappings
	m.OutputMappings = partialMapCmp.OutputMappings

	var cref ComponentReference
	err = cref.UnmarshalBSONValue(bsontype.String, partialMapCmp.Node)
	if err == nil {
		m.Node = cref
		return nil
	}

	var cmpInline Component
	err = bson.Unmarshal(partialMapCmp.Node, &cmpInline)
	if err == nil {
		m.Node = cmpInline
		return nil
	}

	var crefver CRefVersion
	err = bson.Unmarshal(partialMapCmp.Node, &crefver)
	if err == nil {
		m.Node = crefver
		return nil
	}

	return errors.Wrap(err, "could not unmarshal map component")
}

type Conditional struct {
	ImplementationBase `json:",inline" bson:",inline"`
	Expression         `json:"expression" bson:"expression"`
	NodeTrue           interface{} `json:"nodeTrue" bson:"nodeTrue"`
	NodeFalse          interface{} `json:"nodeFalse,omitempty" bson:"nodeFalse,omitempty"`
	InputMappings      []Edge      `json:"inputMappings,omitempty" bson:"inputMappings,omitempty"`
	OutputMappings     []Edge      `json:"outputMappings,omitempty" bson:"outputMappings,omitempty"`
}

func (c *Conditional) UnmarshalJSON(document []byte) error {
	var partialConditional struct {
		ImplementationBase `json:",inline"`
		Expression         Expression      `json:"expression"`
		NodeTrue           json.RawMessage `json:"nodeTrue"`
		NodeFalse          json.RawMessage `json:"nodeFalse,omitempty"`
		InputMappings      []Edge          `json:"inputMappings,omitempty"`
		OutputMappings     []Edge          `json:"outputMappings,omitempty"`
	}

	err := json.Unmarshal(document, &partialConditional)
	if err != nil {
		return errors.Wrapf(err, "cannot unmarshal partial conditional component")
	}
	c.ImplementationBase = partialConditional.ImplementationBase
	c.Expression = partialConditional.Expression
	c.InputMappings = partialConditional.InputMappings
	c.OutputMappings = partialConditional.OutputMappings
	var cref ComponentReference
	var cmpInline Component
	var crefver CRefVersion
	err = json.Unmarshal(partialConditional.NodeTrue, &cref)
	if err == nil {
		c.NodeTrue = cref
	} else {
		err = json.Unmarshal(partialConditional.NodeTrue, &cmpInline)
		if err == nil {
			c.NodeTrue = cmpInline
		} else {
			err = json.Unmarshal(partialConditional.NodeTrue, &crefver)
			if err != nil {
				return errors.Wrapf(err, "cannot unmarshal true node of conditional component")
			}
			c.NodeTrue = crefver
		}
	}

	if partialConditional.NodeFalse != nil { // if false node is not empty
		err = json.Unmarshal(partialConditional.NodeFalse, &cref)
		if err == nil {
			c.NodeFalse = cref
		} else {
			err = json.Unmarshal(partialConditional.NodeFalse, &cmpInline)
			if err == nil {
				c.NodeFalse = cmpInline
			} else {
				err = json.Unmarshal(partialConditional.NodeFalse, &crefver)
				if err != nil {
					return errors.Wrapf(err, "cannot unmarshal false node of conditional component")
				}
				c.NodeFalse = crefver
			}
		}
	}

	return nil
}

func (c *Conditional) UnmarshalBSON(data []byte) error {
	var rawData bson.Raw
	err := bson.Unmarshal(data, &rawData)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal conditional component")
	}
	var partialConditional struct {
		ImplementationBase `bson:",inline"`
		Expression         Expression `bson:"expression"`
		NodeTrue           bson.Raw   `bson:"nodeTrue"`
		NodeFalse          bson.Raw   `bson:"nodeFalse,omitempty"`
		InputMappings      []Edge     `bson:"inputMappings,omitempty"`
		OutputMappings     []Edge     `bson:"outputMappings,omitempty"`
	}

	err = bson.Unmarshal(rawData, &partialConditional)
	if err != nil {
		return errors.Wrap(err, "cannot unmarshal partial conditional component")
	}
	c.ImplementationBase = partialConditional.ImplementationBase
	c.Expression = partialConditional.Expression
	c.InputMappings = partialConditional.InputMappings
	c.OutputMappings = partialConditional.OutputMappings

	var cref ComponentReference
	var crefver CRefVersion
	{
		var trueCmpInline Component
		err = cref.UnmarshalBSONValue(bsontype.String, partialConditional.NodeTrue)
		if err == nil {
			c.NodeTrue = cref
		} else {
			err = bson.Unmarshal(partialConditional.NodeTrue, &trueCmpInline)
			if err == nil {
				c.NodeTrue = trueCmpInline
			} else {
				err = bson.Unmarshal(partialConditional.NodeTrue, &crefver)
				if err != nil {
					return errors.Wrapf(err, "cannot unmarshal true node of conditional component")
				}
				c.NodeTrue = crefver
			}
		}
	}

	if partialConditional.NodeFalse != nil { // if false node is not empty{
		var falseCmpInline Component
		err = cref.UnmarshalBSONValue(bsontype.String, partialConditional.NodeFalse)
		if err == nil {
			c.NodeFalse = cref
		} else {
			err = bson.Unmarshal(partialConditional.NodeFalse, &falseCmpInline)
			if err == nil {
				c.NodeFalse = falseCmpInline
			} else {
				err = bson.Unmarshal(partialConditional.NodeFalse, &crefver)
				if err != nil {
					return errors.Wrapf(err, "cannot unmarshal false node of conditional component")
				}
				c.NodeFalse = crefver
			}
		}
	}

	return nil
}

func WorkspacesInputToCreateData(input workspace.InputData, namespace string) workspace.Data {
	l := make(map[string]string)
	if input.Labels != nil {
		for _, label := range input.Labels {
			l[label[0]] = label[len(label)-1]
		}
	}
	return workspace.Data{
		Name:                input.Name,
		Roles:               input.Roles,
		HideForUnauthorized: input.HideForUnauthorized,
		Labels:              l,
		Namespace:           namespace,
	}
}

func WorkspacesInputToUpdateData(input workspace.InputData, namespace string) workspace.Data {
	l := make(map[string]string)
	if input.Labels != nil {
		for _, label := range input.Labels {
			l[label[0]] = label[len(label)-1]
		}
	}
	return workspace.Data{
		Name:                input.Name,
		Roles:               input.Roles,
		HideForUnauthorized: input.HideForUnauthorized,
		Labels:              l,
		Namespace:           namespace,
	}
}
