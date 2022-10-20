package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/equinor/flowify-workflows-server/models"
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

	rawbytes, err := os.ReadFile(flag.Arg(0))
	if err != nil {
		log.Fatal(err.Error())
		return
	}
	log.SetLevel(log.DebugLevel)
	schema := models.FindSchema(*schemaFilePtr)
	if schema != nil {
		var v interface{}
		if err := json.Unmarshal(rawbytes, &v); err != nil {
			log.Fatal("Cannot unmarshal JSON: ", err.Error())
		}

		err := schema.Validate(v)
		if err != nil {
			log.Fatalf("Validation errorv: %#v", err)
		}
		log.Info("schema validates")
	} else {
		log.Info("schema not validated")
	}
}
