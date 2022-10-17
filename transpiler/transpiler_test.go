package transpiler

import (
	"encoding/json"
	"io/ioutil"
	"path"
	"strconv"

	// "log"
	"testing"

	// wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/pkg/secret"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
)

const (
	minimalExampleJSON = `
	{
		"name": "wf-example",
		"description": "Test workflow with an inline any-component",
		"type": "workflow",
		"workspace": "test",
		"component": {
			"uid": "192161d7-e3f2-4991-adc0-a99c88c144c0",
			"description": "My cool workflow",
			"modifiedBy" : { "oid": "null", "email": "test@test.ts" },
			"inputs": [
				{ "name": "seedT", "mediatype": ["integer"], "type": "parameter" },
				{ "name": "secretWF1", "mediatype": ["string"], "type": "env_secret" },
				{ "name": "secretWF2", "mediatype": ["string"], "type": "env_secret" }
			],
			"outputs": [],
			"type": "component",
			"implementation": {
				"type": "graph",
				"inputMappings": [
					{
						"source": { "port": "seedT" },
						"target": { "node": "N1", "port": "seedN1" }
					},
					{
						"source": { "port": "secretWF1" },
						"target": { "node": "N1", "port": "secretN1" }
					},
					{
						"source": { "port": "secretWF2" },
						"target": { "node": "N1", "port": "secretN2" }
					},
					{
						"source": { "port": "secretWF2" },
						"target": { "node": "N2", "port": "secretN1" }
					},
					{
						"source": { "port": "secretWF2" },
						"target": { "node": "N3", "port": "secretN1" }
					}
			    ],
				"nodes": [
					{
						"id": "N1",
						"node": {
							"uid": "192161d7-e3f2-4991-adc0-a99c88c144b1",
							"description": "B1",
							"inputs": [
								{ "name": "seedN1", "mediatype": ["integer"], "type": "parameter" },
								{ "name": "secretN1", "mediatype": ["string"], "type": "env_secret" },
								{ "name": "secretN2", "mediatype": ["string"], "type": "env_secret" }
							],
							"outputs": [{ "name": "value", "mediatype": ["integer"], "type": "parameter" }],
							"type": "component",
							"implementation": {
								"type": "brick",
								"container": {
									"name": "randgen",
									"image": "alpine:latest",
									"command": ["sh", "-c"],
									"args": []
								},
								"args": [
									{ "source": "sleep 1; " },
									{ "source": "RANDOM=" },
									{
										"source": { "port": "seedN1" },
										"target": { "type": "parameter", "name": "seed" }
									},
									{
										"source": "; RAND_INT=$((1 + RANDOM % 10)); echo $RAND_INT | tee /tmp/output"
									}
								],
								"results": [
									{
										"source": { "file": "/tmp/output" },
										"target": { "port": "value" }
									}
								]
							}
						}
					},
					{
						"id": "N2",
						"node": {
							"uid": "192161d7-e3f2-4991-adc0-a99c88c144b2",
							"description": "B2",
							"inputs": [
								{ "name": "value", "mediatype": ["integer"], "type": "parameter" },
								{ "name": "secretN1", "mediatype": ["string"], "type": "env_secret" }
							],
							"outputs": [{ "name": "value", "mediatype": ["integer"], "type": "parameter" }],
							"type": "component",
							"implementation": {
								"type": "brick",
								"container": {
									"name": "ink",
									"image": "alpine:latest",
									"command": ["sh", "-c"],
									"args": []
								},
								"args": [
									{ "source": "expr " },
									{
										"source": { "port": "value" },
										"target": { "type": "parameter", "name": "x" }
									},
									{ "source": " + 1 | tee /tmp/incd" }
								],
								"results": [
									{
										"source": { "file": "/tmp/incd" },
										"target": { "port": "value" }
									}
								]
							}
						}
					},
					{
						"id": "N3",
						"node": {
							"uid": "192161d7-e3f2-4991-adc0-a99c88c144b2",
							"description": "B2",
							"inputs": [
								{ "name": "value", "mediatype": ["integer"], "type": "parameter" },
								{ "name": "secretN1", "mediatype": ["string"], "type": "env_secret" }
							],
							"outputs": [{ "name": "value", "mediatype": ["integer"], "type": "parameter" }],
							"type": "component",
							"implementation": {
								"type": "brick",
								"container": {
									"name": "ink",
									"image": "alpine:latest",
									"command": ["sh", "-c"],
									"args": []
								},
								"args": [
									{ "source": "expr " },
									{
										"source": { "port": "value" },
										"target": { "type": "parameter", "name": "x" }
									},
									{ "source": " + 1 | tee /tmp/incd" }
								],
								"results": [
									{
										"source": { "file": "/tmp/incd" },
										"target": { "port": "value" }
									}
								]
							}
						}
					}
				],
				"edges": [
					{
						"source": { "node": "N1", "port": "value" },
						"target": { "node": "N2", "port": "value" }
					},
					{
						"source": { "node": "N1", "port": "value" },
						"target": { "node": "N3", "port": "value" }
					}
				]
			}
		}
	}`
)

func init() {
}

func Test_ParseComponentTree(t *testing.T) {
	var wf models.Workflow
	err := json.Unmarshal([]byte(minimalExampleJSON), &wf)
	assert.Nil(t, err)

	assert.Equal(t, "wf-example", wf.Name)
	assert.Equal(t, "test", wf.Workspace)
	assert.Equal(t, models.ComponentType("workflow"), wf.Type)
	assert.Equal(t, models.ComponentType("component"), wf.Component.Type)
	assert.Equal(t, 3, len(wf.Component.Inputs))
	assert.Equal(t, 0, len(wf.Component.Outputs))

	argoWF, err := ParseComponentTree(wf, secretMap{}, volumeMap{}, map[string]string{}, map[string]string{})
	assert.Nil(t, err)
	assert.True(t, len(argoWF.Spec.Templates) == 4, "Expected no. of templates is 4.")
	// assert.Equal(t, "seedWF", argoWF.Spec.Arguments.Parameters[0].Name)
	// assert.Equal(t, "10", argoWF.Spec.Arguments.Parameters[0].Value.String())

	for _, template := range argoWF.Spec.Templates {
		switch template.Name {
		case "192161d7-e3f2-4991-adc0-a99c88c144c0":
			assert.Equal(t, "N1", template.DAG.Tasks[0].Name)
			assert.Equal(t, "N2", template.DAG.Tasks[1].Name)
			assert.Equal(t, "N1", template.DAG.Tasks[1].Dependencies[0])
			assert.Equal(t, "value", template.DAG.Tasks[1].Arguments.Parameters[0].Name)
			assert.Equal(t, "{{tasks.N1.outputs.parameters.value}}", template.DAG.Tasks[1].Arguments.Parameters[0].Value.String())
			assert.Equal(t, "seedT", template.Inputs.Parameters[0].Name)
		case "192161d7-e3f2-4991-adc0-a99c88c144b1":
			assert.Equal(t, 2, len(template.Container.Env))
			for _, env := range template.Container.Env {
				switch env.Name {
				case "secretN1":
					assert.Equal(t, "secretWF1", env.ValueFrom.SecretKeyRef.Key)
				case "secretN2":
					assert.Equal(t, "secretWF2", env.ValueFrom.SecretKeyRef.Key)
				default:
					t.Errorf("Unexpected secret %s in brick %s.", env.Name, template.Name)
				}
			}
		case "192161d7-e3f2-4991-adc0-a99c88c144b2":
			assert.Equal(t, 1, len(template.Container.Env))
			assert.Equal(t, "secretN1", template.Container.Env[0].Name)
			assert.Equal(t, "secretWF2", template.Container.Env[0].ValueFrom.SecretKeyRef.Key)
		default:
			t.Errorf("Unexpected template %s.", template.Name)
		}
	}
}

func Test_RemoveDuplicatedTemplates(t *testing.T) {
	var wf models.Workflow
	err := json.Unmarshal([]byte(minimalExampleJSON), &wf)
	assert.Nil(t, err)

	job := models.Job{Metadata: models.Metadata{Description: "test job"}, Type: "job", InputValues: nil, Workflow: wf}
	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.True(t, len(argoWF.Spec.Templates) == 3, "Expected no. of templates is 3.")
}

func Test_GetArgoWorkflow(t *testing.T) {
	raw, err := ioutil.ReadFile("../models/examples/job-example.json")
	assert.Nil(t, err)
	var job models.Job
	err = json.Unmarshal(raw, &job)
	assert.Nil(t, err)

	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.Equal(t, "wf-example", argoWF.Name)
	assert.Equal(t, "sandbox-project-a", argoWF.Namespace)

	assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144c0", argoWF.Spec.Entrypoint)
	assert.Equal(t, "seedT", argoWF.Spec.Arguments.Parameters[0].Name)
	assert.Equal(t, "10", argoWF.Spec.Arguments.Parameters[0].Value.String())

	for _, template := range argoWF.Spec.Templates {
		switch template.Name {
		case "192161d7-e3f2-4991-adc0-a99c88c144c0":
			assert.Equal(t, "seedT", template.Inputs.Parameters[0].Name)
			for _, task := range template.DAG.Tasks {
				switch task.Name {
				case "N1":
					assert.Equal(t, "seedN1", task.Arguments.Parameters[0].Name)
					assert.Equal(t, "{{inputs.parameters.seedT}}", task.Arguments.Parameters[0].Value.String())
				case "N2":
					assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144c2", task.Template)
				}
			}
		case "192161d7-e3f2-4991-adc0-a99c88c144b1":
			assert.Equal(t, "seedN1", template.Inputs.Parameters[0].Name)
			for _, env := range template.Container.Env {
				switch env.Name {
				case "secretB1":
					assert.Equal(t, "SECRET_PASS", env.ValueFrom.SecretKeyRef.Key)
				case "secretB2":
					assert.Equal(t, "SECRET_ID", env.ValueFrom.SecretKeyRef.Key)
				}
			}
		case "192161d7-e3f2-4991-adc0-a99c88c144b2":
			assert.Equal(t, 0, len(template.Inputs.Parameters))
			for _, env := range template.Container.Env {
				switch env.Name {
				case "secretW1":
					assert.Equal(t, "SECRET_PASS", env.ValueFrom.SecretKeyRef.Key)
				case "secretW2":
					assert.Equal(t, "SECRET_ID", env.ValueFrom.SecretKeyRef.Key)
				case "secretW3":
					assert.Equal(t, "SECRET_VAL", env.ValueFrom.SecretKeyRef.Key)
				}
			}
			assert.Equal(t, 1, len(template.Outputs.Artifacts))
			assert.Equal(t, "artifactVal", template.Outputs.Artifacts[0].Name)
			assert.Equal(t, "/tmp/artifact", template.Outputs.Artifacts[0].Path)
		case "192161d7-e3f2-4991-adc0-a99c88c144c2":
			assert.Equal(t, 2, len(template.DAG.Tasks))
			for _, task := range template.DAG.Tasks {
				switch task.Name {
				case "N2G2B2":
					assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144b2", task.Template)
				case "N3":
					assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144b3", task.Template)
					assert.Equal(t, 1, len(task.Arguments.Artifacts))
					assert.Equal(t, "artifVal", task.Arguments.Artifacts[0].Name)
					assert.Equal(t, "{{tasks.N2G2B2.outputs.artifacts.artifactVal}}", task.Arguments.Artifacts[0].From)
					assert.Equal(t, 2, len(task.Arguments.Parameters))
					for _, p := range task.Arguments.Parameters {
						switch p.Name {
						case "val":
							assert.Equal(t, "{{inputs.parameters.seedMain}}", p.Value.String())
						case "paramVal":
							assert.Equal(t, "{{tasks.N2G2B2.outputs.parameters.value}}", p.Value.String())
						}
					}
				}
			}
			assert.Equal(t, 1, len(template.Outputs.Artifacts))
			assert.Equal(t, "artifactVal", template.Outputs.Artifacts[0].Name)
			assert.Equal(t, "{{tasks.N3.outputs.artifacts.artifactVal}}", template.Outputs.Artifacts[0].From)
			assert.Equal(t, 1, len(template.Outputs.Parameters))
			assert.Equal(t, "value", template.Outputs.Parameters[0].Name)
			assert.Equal(t, "{{tasks.N3.outputs.parameters.parameterVal}}", template.Outputs.Parameters[0].ValueFrom.Parameter)
		case "192161d7-e3f2-4991-adc0-a99c88c144c3":
			assert.Equal(t, 2, len(template.DAG.Tasks))
			assert.Equal(t, "valFromArtifact", template.Inputs.Artifacts[0].Name)
			assert.Equal(t, "valFromParam", template.Inputs.Parameters[0].Name)
			assert.Equal(t, 0, len(template.Outputs.Artifacts))
			assert.Equal(t, 0, len(template.Outputs.Parameters))
			for _, task := range template.DAG.Tasks {
				switch task.Name {
				case "N4G3B1":
					assert.Equal(t, 0, len(task.Arguments.Artifacts))
					assert.Equal(t, "valParam", task.Arguments.Parameters[0].Name)
					assert.Equal(t, "{{inputs.parameters.valFromParam}}", task.Arguments.Parameters[0].Value.String())
					assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144b4", task.Template)
				case "N4G3B2":
					assert.Equal(t, 0, len(task.Arguments.Parameters))
					assert.Equal(t, "valArtifact", task.Arguments.Artifacts[0].Name)
					assert.Equal(t, "{{inputs.artifacts.valFromArtifact}}", task.Arguments.Artifacts[0].From)
					assert.Equal(t, "192161d7-e3f2-4991-adc0-a99c88c144b5", task.Template)
				}
			}
		case "192161d7-e3f2-4991-adc0-a99c88c144b4":
			assert.Equal(t, "valParam", template.Inputs.Parameters[0].Name)
			assert.Equal(t, "secretPASS", template.Container.Env[0].Name)
			assert.Equal(t, "SECRET_PASS", template.Container.Env[0].ValueFrom.SecretKeyRef.Key)
			assert.Equal(t, secret.DefaultObjectName, template.Container.Env[0].ValueFrom.SecretKeyRef.Name)
		case "192161d7-e3f2-4991-adc0-a99c88c144b5":
			assert.Equal(t, "valArtifact", template.Inputs.Artifacts[0].Name)
			assert.Equal(t, "/artifacts/valArtifact", template.Inputs.Artifacts[0].Path)
			assert.Equal(t, 0, len(template.Outputs.Artifacts))
			assert.Equal(t, 0, len(template.Outputs.Parameters))
			assert.Equal(t, 1, len(template.Container.Args))
			assert.Equal(t, "--requirement={{inputs.artifacts.valArtifact.path}}", template.Container.Args[0])
		}
	}
}

func Test_TranspileVolume(t *testing.T) {

	whalesay := v1.Container{
		Image:   "docker/whalesay",
		Command: []string{"cowsay"},
		Args:    []string{"hello mount"},
	}

	vols := map[string]v1.Volume{"vol-config-0": {Name: "vol-config-0"}, "vol-config-1": {Name: "vol-config-1"}}

	volConf1, err := json.Marshal(vols["vol-config-0"])
	assert.Nil(t, err)

	volConf2, err := json.Marshal(vols["vol-config-1"])
	assert.Nil(t, err)

	prefix := "vols/mypath"
	postfix := ""
	portName := "mount"
	job := models.Job{
		Type: "job",
		InputValues: []models.Value{
			{Value: string(volConf1), Target: portName + "-0"},
			{Value: string(volConf2), Target: portName + "-1"}},
		Workflow: models.Workflow{
			Type:      "workflow",
			Workspace: "test",
			Component: models.Component{
				ComponentBase: models.ComponentBase{
					Metadata: models.Metadata{Uid: models.NewComponentReference()},
					Inputs: []models.Data{
						{Name: portName + "-0", MediaType: nil, Type: "volume"},
						{Name: portName + "-1", MediaType: nil, Type: "volume"}},
					Outputs: []models.Data{},
					Type:    "component",
				},
				Implementation: models.Brick{
					ImplementationBase: models.ImplementationBase{Type: "brick"},
					Container:          &whalesay,
					Args: []models.Argument{
						{
							ArgumentBase: models.ArgumentBase{Target: models.ArgumentTarget{Type: "volume", Prefix: prefix, Suffix: postfix}},
							Source:       models.ArgumentSourcePort{Port: portName + "-0"},
						},
						{
							ArgumentBase: models.ArgumentBase{Target: models.ArgumentTarget{Type: "volume", Prefix: prefix, Suffix: postfix}},
							Source:       models.ArgumentSourcePort{Port: portName + "-1"},
						},
					},
					Results: []models.Result{},
				},
			},
		},
	}
	ignore(job)

	// transpile
	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.NotNil(t, argoWF)

	assert.Equal(t, len(vols), len(argoWF.Spec.Volumes), "expects matching configs")
	// order of volumes after transpilation is not stable because maps use randomized range-iteration
	for _, v := range argoWF.Spec.Volumes {
		// check transpilation against input
		assert.Equal(t, v, vols[v.Name])
	}

	// the mount path is the concatenation of prefix + component-port-name + postfix
	for i, v := range argoWF.Spec.Templates[0].Container.VolumeMounts {
		assert.Equal(t, v.MountPath, path.Join(prefix, portName+"-"+strconv.Itoa(i), postfix))
	}
	//assert.NotNil(t, nil, string(first(json.MarshalIndent(job, "  ", ""))))
}

func Test_TranspileGraphVolume(t *testing.T) {

	raw, err := ioutil.ReadFile("../models/examples/graph-input-volumes.json")
	assert.Nil(t, err)
	var job models.Job
	err = json.Unmarshal(raw, &job)
	assert.Nil(t, err)

	// transpile
	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.NotNil(t, argoWF)

	assert.Equal(t, 2, len(argoWF.Spec.Volumes), "we expect two volumes in config")
	assert.Equal(t, "/opt/volumes/mount-a", argoWF.Spec.Templates[1].Container.VolumeMounts[0].MountPath, "mounted at specific paths")
	assert.Equal(t, "/mnt/mount-b", argoWF.Spec.Templates[1].Container.VolumeMounts[1].MountPath, "mounted at specific paths")

	//assert.NotNil(t, nil, string(first(json.MarshalIndent(argoWF, "  ", ""))))
	ignore(first(json.MarshalIndent(argoWF, "  ", ""))) // silence unused warning
}

func Test_TranspileGraphThroughputVolume(t *testing.T) {

	raw, err := ioutil.ReadFile("../models/examples/graph-throughput-volumes.json")
	assert.Nil(t, err)
	var job models.Job
	err = json.Unmarshal(raw, &job)
	assert.Nil(t, err)

	// transpile

	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.NotNil(t, argoWF)

	assert.Equal(t, 1, len(argoWF.Spec.Volumes), "we expect one volume in config")
	assert.Equal(t, "/opt/volumes/greeting", argoWF.Spec.Templates[1].Container.VolumeMounts[0].MountPath, "mounted at specific path")

	//	assert.NotNil(t, nil, string(first(json.MarshalIndent(argoWF, "  ", ""))))
	ignore(first(json.MarshalIndent(argoWF, "  ", ""))) // silence unused warning
}

func ignore[T any](arg1 T) {}

func first[T1 any, T2 any](arg1 T1, arg2 T2) T1 { return arg1 }

func Test_TranspileIfStatement(t *testing.T) {

	raw, err := ioutil.ReadFile("../models/examples/if-statement.json")
	assert.Nil(t, err)
	var job models.Job
	err = json.Unmarshal(raw, &job)
	assert.Nil(t, err)

	// transpile
	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.NotNil(t, argoWF)

	assert.Equal(t, 4, len(argoWF.Spec.Templates), "we expect 4 templates, cf. example file")
	template := argoWF.Spec.Templates[2] // template containing if statement
	assert.Equal(t, 1, len(template.DAG.Tasks), "we expect one node in conditional DAG")
	assert.NotEqual(t, "", template.DAG.Tasks[0].When)
	assert.NotEqual(t, "", template.Outputs.Parameters[0].ValueFrom.Expression)
}

func Test_TranspileIfElseStatement(t *testing.T) {

	raw, err := ioutil.ReadFile("../models/examples/if-else-statement.json")
	assert.Nil(t, err)
	var job models.Job
	err = json.Unmarshal(raw, &job)
	assert.Nil(t, err)

	// transpile
	argoWF, err := GetArgoWorkflow(job)
	assert.Nil(t, err)
	assert.NotNil(t, argoWF)

	assert.Equal(t, 5, len(argoWF.Spec.Templates), "we expect 4 templates, cf. example file")
	template := argoWF.Spec.Templates[2] // template containing if statement
	assert.Equal(t, 2, len(template.DAG.Tasks), "we expect two nodes in conditional DAG")
	assert.NotEqual(t, "", template.DAG.Tasks[0].When)
	assert.NotEqual(t, "", template.DAG.Tasks[1].When)
	assert.NotEqual(t, "", template.Outputs.Parameters[0].ValueFrom.Expression)
}
