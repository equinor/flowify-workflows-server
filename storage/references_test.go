package storage

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/stretchr/testify/assert"
)

const (
	cmpRefJson = `"192161d7-e3f2-4991-adc0-a99c88c144b2"`
	brickJson  = `
{
  "uid": "192161d7-e3f2-4991-adc0-a99c88c144b2",
  "description": "B2",
  "version": {
	"current": 1,
	"tags": ["latest"]
  },
  "inputs": [],
  "outputs": [],
  "type": "component",
  "implementation": {
	"type": "brick",
	"container": {
	  "name": "containername",
	  "image": "docker/whalesay",
	  "command": ["cowsay"],
	  "args": ["Hello from B2"]
	}
  }
}`
	graphJson = `
{
  "uid": "192161d7-e3f2-4991-adc0-a99c88c144c0",
  "description": "My cool workflow",
  "inputs": [],
  "outputs": [],
  "type": "component",
  "implementation": {
    "type": "graph",
	"inputMappings": [],
	"nodes": [
	  {
	    "id": "N1",
		"node": {
		  "uid": "192161d7-e3f2-4991-adc0-a99c88c144b1",
		  "description": "B1",
		  "inputs": [],
		  "outputs": [],
		  "type": "component",
		  "implementation": {
			"type": "brick",
			"container": {
			  "name": "containername_n1_b1",
			  "image": "docker/whalesay",
			  "command": ["cowsay"],
			  "args": ["Hello from B1"]
			},
			"args": []
		  }
	    }
	  },
	  {
		"id": "N2",
		"node": {
		  "uid": "192161d7-e3f2-4991-adc0-a99c88c144c2",
		  "description": "G2",
		  "inputs": [],
		  "outputs": [],
		  "type": "component",
		  "implementation": {
			"type": "graph",
			"nodes": [
			  {
				"id": "N2G2B2",
				"node": "192161d7-e3f2-4991-adc0-a99c88c144b2"
			  },
			  {
				"id": "N2M1B1",
				"node": "192161d7-e3f2-4991-adc0-a99c88c144c3"
			  }
			],
			"edges": []
		  }
		}
  	  },
	  {
		"id": "N3",
		"node": "192161d7-e3f2-4991-adc0-a99c88c144b2"
	  }
	],
	"edges": []
  }
}`

	mapJson = `
{
	"uid": "192161d7-e3f2-4991-adc0-a99c88c144c3",
    "description": "Inner map",
    "inputs": [],
    "outputs": [],
    "type": "component",
    "implementation": {
        "type": "map",
        "node": {
			"version": 2,
			"uid": "192161d7-e3f2-4991-adc0-a88c99c144b1"
		}
	}
}`

	mapNodeJson1 = `
{
	"uid": "192161d7-e3f2-4991-adc0-a88c99c144b1",
	"description": "MapNode2",
	"inputs": [],
	"outputs": [],
	"type": "component",
	"implementation": {
		"type": "brick",
		"container": {
			"name": "containername_n1_b1",
			"image": "alpine:0.1",
			"command": ["sh", "-c", "echo $0 | tee /tmp/prm"],
			"args": []
		},
		"args": [],
		"results": []
	}
}
`

	mapNodeJson2 = `
{
	"uid": "192161d7-e3f2-4991-adc0-a88c99c144b1",
	"description": "MapNode2",
	"inputs": [],
	"outputs": [],
	"type": "component",
	"implementation": {
		"type": "brick",
		"container": {
			"name": "containername_n1_b1",
			"image": "alpine:0.2",
			"command": ["sh", "-c", "echo $0 | tee /tmp/prm"],
			"args": []
		},
		"args": [],
		"results": []
	}
}
`

	expectedGraphJson = `
{
  "uid": "192161d7-e3f2-4991-adc0-a99c88c144c0",
  "description": "My cool workflow",
  "inputs": [],
  "outputs": [],
  "type": "component",
  "implementation": {
    "type": "graph",
	"inputMappings": [],
	"nodes": [
	  {
	    "id": "N1",
		"node": {
		  "uid": "192161d7-e3f2-4991-adc0-a99c88c144b1",
		  "description": "B1",
		  "inputs": [],
		  "outputs": [],
		  "type": "component",
		  "implementation": {
			"type": "brick",
			"container": {
			  "name": "containername_n1_b1",
			  "image": "docker/whalesay",
			  "command": ["cowsay"],
			  "args": ["Hello from B1"]
			},
			"args": []
		  }
	    }
	  },
	  {
		"id": "N2",
		"node": {
		  "uid": "192161d7-e3f2-4991-adc0-a99c88c144c2",
		  "description": "G2",
		  "inputs": [],
		  "outputs": [],
		  "type": "component",
		  "implementation": {
			"type": "graph",
			"nodes": [
			  {
				"id": "N2G2B2",
				"node": {
				  "uid": "192161d7-e3f2-4991-adc0-a99c88c144b2",
				  "version": {
					"current": 1,
					"tags": ["latest"]
				  },
				  "description": "B2",
				  "inputs": [],
				  "outputs": [],
				  "type": "component",
				  "implementation": {
					"type": "brick",
					"container": {
					  "name": "containername",
					  "image": "docker/whalesay",
					  "command": ["cowsay"],
					  "args": ["Hello from B2"]
					}
				  }
				}
			  },
			  {
			    "id": "N2M1B1",
				"node": {
				  "uid": "192161d7-e3f2-4991-adc0-a99c88c144c3",
				  "version": {
					"current": 1,
					"tags": ["latest"]
				  },
				  "description": "Inner map",
				  "inputs": [],
				  "outputs": [],
				  "type": "component",
				  "implementation": {
					  "type": "map",
					  "node": {
						"uid": "192161d7-e3f2-4991-adc0-a88c99c144b1",
						"version": {
						  "current": 2,
						  "tags": ["latest"],
						  "previous": {
							"version": 1
						  }
						},
						"description": "MapNode2",
						"inputs": [],
						"outputs": [],
						"type": "component",
						"implementation": {
						  "type": "brick",
						  "container": {
						    "name": "containername_n1_b1",
							"image": "alpine:0.2",
							"command": ["sh", "-c", "echo $0 | tee /tmp/prm"],
							"args": []
						  },
						  "args": [],
						  "results": []
						}
					  }
				  }
				}
			  }
			],
			"edges": []
		  }
		}
  	  },
	  {
		"id": "N3",
		"node": {
		  "uid": "192161d7-e3f2-4991-adc0-a99c88c144b2",
		  "version": {
			"current": 1,
			"tags": ["latest"]
		  },
		  "description": "B2",
		  "inputs": [],
		  "outputs": [],
		  "type": "component",
		  "implementation": {
			"type": "brick",
			"container": {
			  "name": "containername",
			  "image": "docker/whalesay",
			  "command": ["cowsay"],
			  "args": ["Hello from B2"]
			}
		  }
		}
	  }
	],
	"edges": []
  }
}`
)

var brickCmp models.Component
var graphCmp models.Component
var mapCmp models.Component
var mapNodeCmp1 models.Component
var mapNodeCmp2 models.Component
var cmpRef models.ComponentReference
var expectedGraphCmp models.Component

func init() {
	err := json.Unmarshal([]byte(brickJson), &brickCmp)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(graphJson), &graphCmp)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(cmpRefJson), &cmpRef)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(mapJson), &mapCmp)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(mapNodeJson1), &mapNodeCmp1)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(mapNodeJson2), &mapNodeCmp2)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal([]byte(expectedGraphJson), &expectedGraphCmp)
	if err != nil {
		panic(err)
	}
}

func first(i int, e error) int { return i }

func TestDereferenceComponent(t *testing.T) {
	cfg := DbConfig{
		DbName: test_db_name,
		Select: "mongo",
		Config: map[string]interface{}{
			"Address": os.Getenv("FLOWIFY_DB_CONFIG_ADDRESS"),
			"Port":    first(strconv.Atoi(os.Getenv("FLOWIFY_DB_CONFIG_PORT")))},
	}

	cstorage := NewMongoStorageClient(NewMongoClient(cfg), cfg.DbName)
	err := cstorage.CreateComponent(context.TODO(), brickCmp)
	assert.Nil(t, err)
	err = cstorage.CreateComponent(context.TODO(), graphCmp)
	assert.Nil(t, err)
	err = cstorage.CreateComponent(context.TODO(), mapCmp)
	assert.Nil(t, err)
	err = cstorage.CreateComponent(context.TODO(), mapNodeCmp1)
	assert.Nil(t, err)
	err = cstorage.PutComponent(context.TODO(), mapNodeCmp2)
	assert.Nil(t, err)

	// Test dereference of ComponentReference
	cmpObj, err := DereferenceComponent(context.TODO(), cstorage, cmpRef)
	assert.Nil(t, err)
	assert.Equal(t, brickCmp, cmpObj)

	// Test dereference of Component with nested ComponentReferences
	cmpObj, err = DereferenceComponent(context.TODO(), cstorage, graphCmp)
	assert.Nil(t, err)
	assert.Equal(t, expectedGraphCmp, cmpObj)
}
