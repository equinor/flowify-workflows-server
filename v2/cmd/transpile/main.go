package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"sigs.k8s.io/yaml"

	// "github.com/google/uuid"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/transpiler"
	log "github.com/sirupsen/logrus"
)

func myUsage() {
	fmt.Printf("Usage: %s [OPTIONS] filename\n", os.Args[0])
	flag.PrintDefaults()
}

func main() {
	log.SetLevel(log.InfoLevel)

	schemaFilePtr := flag.String("schema", "", "a file path")
	flag.Parse()
	flag.Usage = myUsage
	if flag.NArg() > 1 {
		flag.Usage()
		return
	}

	rawbytes, err := ioutil.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err.Error())
		return
	}

	schema := models.FindSchema(*schemaFilePtr)
	log.Infof("schema from: %s", *schemaFilePtr)

	if schema != nil {
		log.Infof("schema: %s", *schemaFilePtr)
		var v interface{}
		if err := json.Unmarshal(rawbytes, &v); err != nil {
			log.Fatalf(err.Error())
		}

		err := schema.Validate(v)
		if err != nil {
			b := fmt.Sprintf("%#v\n", err)
			log.Fatal(string(b))
		}
		log.Info("schema validates")

	}
	var job models.Job
	var workflow models.Workflow
	var component models.Component

	err = json.Unmarshal(rawbytes, &job)
	if err == nil && job.Type == models.ComponentType("job") {
		log.Info("Job in the input file.")
	} else {
		err = json.Unmarshal(rawbytes, &workflow)
		if err == nil && workflow.Type == "workflow" {
			log.Info("Workflow in the input file.")
			job = models.Job{Metadata: models.Metadata{Description: "Empty job from workflow"}, Type: "job", InputValues: nil, Workflow: workflow}
		} else {
			err = json.Unmarshal(rawbytes, &component)
			if err == nil {
				log.Info("Component in the input file.")
				workflow = models.Workflow{Metadata: models.Metadata{}, Component: component, Workspace: ""}
				job = models.Job{Metadata: models.Metadata{Description: "Empty job from component"}, Type: "job", InputValues: nil, Workflow: workflow}
			} else {
				log.Fatal("Can't convert file content to Job/Workflow/Component object.")
			}
		}
	}

	ajob, err := transpiler.GetArgoWorkflow(job)
	if err != nil {
		log.Fatal(err.Error())
	}

	outBytes, _ := yaml.Marshal(ajob)
	fmt.Print(string(outBytes))
}
