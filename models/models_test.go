package models

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"go.mongodb.org/mongo-driver/bson"
)

func init() {
}

func Test_CRefRoundtrip(t *testing.T) {
	doc := struct {
		Id uuid.UUID `bson:"id"`
	}{uuid.New()}
	{
		// test bson roundtrip
		raw, err := bson.Marshal(doc)
		assert.Nil(t, err)

		var doc2 struct {
			Id uuid.UUID `bson:"id"`
		}
		err = bson.Unmarshal(raw, &doc2)
		assert.Nil(t, err)
		assert.Equal(t, doc, doc2)
	}
}

func Test_NodeRoundtrip(t *testing.T) {
	const rawInline = `
{
	"id": "A",
	"node": {
		"description": "A brick component",
		"type": "component",
		"implementation": {
			"type": "brick",
			"container": {
				"name": "containername",
				"image": "docker/whalesay",
				"command": ["cowsay"],
				"args": ["hello world"]
			}
		}
	},
	"userdata": {"tags":["tag1","tag2"]}
}`

	const rawRef = `
{
	"id": "A",
	"node": "49f9a952-a375-11ec-b909-0242ac120002"
}`

	const rawRefVer = `
{
	"id": "A",
	"node": {
		"uid": "49f9a952-a375-11ec-b909-0242ac120002",
		"version": 1
	}
}`
	var nodeTests = []struct {
		Name string
		raw  string      // input
		impl interface{} // expected type
	}{
		{"inline", rawInline, Component{}},
		{"ref", rawRef, ComponentReference{}},
		{"refVer", rawRefVer, CRefVersion{}},
	}

	for _, test := range nodeTests {
		t.Run(test.Name, func(t *testing.T) {
			var n Node
			err := json.Unmarshal([]byte(test.raw), &n)
			assert.Nil(t, err)
			assert.IsType(t, n.Node, test.impl)

			raw, err := json.Marshal(n)
			assert.Nil(t, err)
			assert.NotNil(t, raw)

			var node2 Node
			err = json.Unmarshal(raw, &node2)
			assert.Nil(t, err)
			assert.Equal(t, n, node2)
			{
				// test bson roundtrip
				raw2, err := bson.Marshal(n)
				assert.Nil(t, err)

				var n2 Node
				err = bson.Unmarshal(raw2, &n2)
				assert.Nil(t, err)
				assert.Equal(t, n, n2)
			}
		})
	}
}

func Test_NilUnmarshall(t *testing.T) {
	var cmp *Component
	assert.Nil(t, cmp, "(uninitialized) pointer is nil")
	err := json.Unmarshal([]byte("{}"), cmp)
	assert.NotNil(t, err,
		"Expl: if a nil pointer was used in our custom marshaller, it would segfault-panic")
}

func Test_ExprRoundtrip(t *testing.T) {
	var exprTests = []struct {
		name string
		expr string // input
	}{
		{
			name: "Expr0",
			expr: `{
				"left": "4",
				"operator": ">=",
				"right": "5"
			}`,
		},
		{
			name: "Expr1",
			expr: `{
				"left": {
				"name": "valFromParam",
				"mediatype": ["number"],
				"type": "parameter"
				},
				"operator": ">=",
				"right": "5"
			}`,
		},
	}
	for _, test := range exprTests {
		t.Run(test.name, func(t *testing.T) {
			var expr Expression
			RoundtripFromBytes([]byte(test.expr), &expr, t, nil)
		})
	}
}

type TypeTest = func(item any, t *testing.T)

func RoundtripFromFile(filename string, item any, t *testing.T, typeTest TypeTest) {

	raw, err := os.ReadFile(filename)
	assert.Nil(t, err)

	RoundtripFromBytes(raw, item, t, typeTest)
}

func RoundtripFromBytes(raw []byte, item any, t *testing.T, typeTest TypeTest) {

	// 1. unmarshal into type
	err := json.Unmarshal(raw, item)
	assert.Nil(t, err)

	// 2. run extra tests on unmarshaled type here, for example to ensure special field-values exists
	if typeTest != nil {
		typeTest(item, t)
	}

	// 3. marshal back into bytes
	raw2, err := json.Marshal(item)
	assert.Nil(t, err)

	// 4. Read back into copy
	item2 := item // copy
	err = json.Unmarshal(raw2, item2)
	assert.Nil(t, err)

	// 5. Compare roundtripped to initial
	assert.Equal(t, item, item2)

	// 6. do the same for BSON marshaller
	{
		// test bson roundtrip
		raw3, err := bson.Marshal(item)
		assert.Nil(t, err)

		item3 := item
		err = bson.Unmarshal(raw3, item3)
		fmt.Printf("%#v", err)
		assert.Nil(t, err)
		assert.Equal(t, item, item3)
	}
}

func Test_JobRoundtrip(t *testing.T) {
	var jobTests = []struct {
		filename string // input
	}{
		// ADD RELEVANT JOB-EXAMPLES HERE, IMPORTANT FOR TEST COVERAGE
		{"examples/graph-input-volumes.json"},
		{"examples/graph-throughput-volumes.json"},
		{"examples/if-else-statement.json"},
		{"examples/if-statement.json"},
		{"examples/job-example.json"},
		{"examples/job-map-example.json"},
		{"examples/job-submap-example.json"},
	}
	for _, test := range jobTests {
		t.Run(test.filename, func(t *testing.T) {
			var job Job
			RoundtripFromFile(test.filename, &job, t, func(item any, t *testing.T) { assert.Equal(t, item.(*Job).Type, ComponentType("job")) })
		})
	}
}

func Test_WorkflowRoundtrip(t *testing.T) {
	var wfTests = []struct {
		filename string // input
	}{
		{"examples/minimal-any-workflow.json"},
		{"examples/hello-world-workflow.json"},
	}
	for _, test := range wfTests {
		t.Run(test.filename, func(t *testing.T) {
			var wf Workflow
			RoundtripFromFile(test.filename, &wf, t, func(item any, t *testing.T) { assert.Equal(t, item.(*Workflow).Type, ComponentType("workflow")) })
		})
	}
}

func Test_ComponentRoundtrip(t *testing.T) {
	var cmpTests = []struct {
		filename string      // input
		impl     interface{} // expected type
	}{
		{"examples/minimal-any-component.json", Any{}},
		{"examples/minimal-brick-component.json", Brick{}},
		{"examples/minimal-graph-component.json", Graph{}},
		{"examples/minimal-map-component.json", Map{}},
		{"examples/minimal-conditional-component.json", Conditional{}},
		{"examples/single-node-graph-component.json", Graph{}},
		{"examples/two-node-graph-component.json", Graph{}},
		{"examples/two-node-graph-component-with-cref.json", Graph{}},
		{"examples/brick-parameter-component.json", Brick{}},
	}
	for _, test := range cmpTests {
		t.Run(test.filename, func(t *testing.T) {
			logrus.Info(test.filename)
			var cmp Component
			RoundtripFromFile(test.filename, &cmp, t, func(item any, t *testing.T) { assert.Equal(t, ComponentType("component"), item.(*Component).Type) })
		})
	}
}

func Test_GraphMarshal(t *testing.T) {
	g := Graph{ImplementationBase: ImplementationBase{Type: "graph"}}
	bytes, err := json.Marshal(g)
	assert.Nil(t, err)
	assert.Equal(t, []byte(`{"type":"graph"}`), bytes)
}

func Test_ComponentSpec(t *testing.T) {
	var specTests = []struct {
		filename   string // input
		schemaFile string // to validate against
	}{
		{"examples/minimal-any-component.json", "spec/component.schema.json"},
		{"examples/minimal-graph-component.json", "spec/component.schema.json"},
		{"examples/minimal-map-component.json", "spec/component.schema.json"},
		{"examples/single-node-graph-component.json", "spec/component.schema.json"},
		{"examples/two-node-graph-component.json", "spec/component.schema.json"},
		{"examples/minimal-brick-component.json", "spec/component.schema.json"},
		{"examples/brick-parameter-component.json", "spec/component.schema.json"},
		{"examples/multi-level-secrets.json", "spec/component.schema.json"},
		// wfs
		{"examples/minimal-any-workflow.json", "spec/workflow.schema.json"},
		{"examples/hello-world-workflow.json", "spec/workflow.schema.json"},
		// jobs
		{"examples/job-example.json", "spec/job.schema.json"},
		{"examples/job-map-example.json", "spec/job.schema.json"},
		{"examples/job-submap-example.json", "spec/job.schema.json"},
		{"examples/job-mounts.json", "spec/job.schema.json"},
		{"examples/graph-input-volumes.json", "spec/job.schema.json"},
	}
	for _, test := range specTests {
		t.Run(test.filename, func(t *testing.T) {
			raw, err := os.ReadFile(test.filename)
			assert.Nil(t, err)

			// non-nil on error
			res := Validate(raw, test.schemaFile)

			assert.Nil(t, res)
		})
	}
}

func Test_Argument(t *testing.T) {
	const raw1 = `
	{
		"source": "justString"
	}`
	const raw2 = `
	{
		"source": { "port": "inputPort" },
		"target": { "type": "parameter"}
	}`
	const raw3 = `
	{
		"source": { "port": "inputPort" },
		"target": {
			"type": "parameter",
			"prefix": "prefix"
		}
	}`
	const raw4 = `
	{
		"source": { "port": "inputPort" },
		"target": {
			"type": "artifact",
			"suffix": "suffix"
		}
	}`

	var argsTests = []struct {
		rawData  string // input
		expected Argument
	}{
		{raw1, Argument{Source: "justString"}},
		{raw2, Argument{Source: ArgumentSourcePort{Port: "inputPort"}, ArgumentBase: ArgumentBase{Target: ArgumentTarget{Type: FlowifyParameterType}}}},
		{raw3, Argument{Source: ArgumentSourcePort{Port: "inputPort"}, ArgumentBase: ArgumentBase{Target: ArgumentTarget{Type: FlowifyParameterType, Prefix: "prefix"}}}},
		{raw4, Argument{Source: ArgumentSourcePort{Port: "inputPort"}, ArgumentBase: ArgumentBase{Target: ArgumentTarget{Type: FlowifyArtifactType, Suffix: "suffix"}}}},
	}

	for _, test := range argsTests {
		var arg Argument
		RoundtripFromBytes([]byte(test.rawData), &arg, t, func(item any, t *testing.T) {
			assert.Equal(t, &test.expected, item.(*Argument))
		})
	}
}

func Test_Version(t *testing.T) {
	var cmp Component
	raw, err := os.ReadFile("examples/minimal-map-component.json")
	assert.Nil(t, err)
	err = json.Unmarshal(raw, &cmp)
	assert.Nil(t, err)

	var argsTests = []struct {
		actual   Version
		expected Version
	}{
		{cmp.Version, Version{Current: VersionNumber(5), Tags: []string{"tag1", "tag2"}, Previous: CRefVersion{Version: VersionNumber(10), Uid: ComponentReference(uuid.MustParse("192161d7-e3f2-4991-adc0-a99c88c144c0"))}}},
		{cmp.Implementation.(Map).Node.(Component).Version, Version{Current: VersionNumber(1), Tags: []string{"tag3", "tag2"}, Previous: CRefVersion{Version: VersionNumber(0)}}},
	}

	for _, test := range argsTests {
		assert.Equal(t, test.expected, test.actual)
	}
}
