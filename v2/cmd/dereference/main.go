package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"

	"github.com/equinor/flowify-workflows-server/models"
	"github.com/equinor/flowify-workflows-server/v2/storage"
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

const (
	db_host                = "localhost"
	db_port                = 27017
	ext_mongo_hostname_env = "FLOWIFY_MONGO_ADDRESS"
	ext_mongo_port_env     = "FLOWIFY_MONGO_PORT"
)

func init() {
	if _, exists := os.LookupEnv(ext_mongo_hostname_env); !exists {
		os.Setenv(ext_mongo_hostname_env, db_host)
	}

	if _, exists := os.LookupEnv(ext_mongo_port_env); !exists {
		os.Setenv(ext_mongo_port_env, strconv.Itoa(db_port))
	}
}

func myUsage() {
	fmt.Printf("Usage: %s [OPTIONS] [cmpRef]\n", os.Args[0])
	flag.PrintDefaults()
}

// read a reference or an inline component
func parseInput(doc []byte) (interface{}, error) {
	{
		// try a plain reference
		cref, err := uuid.ParseBytes(bytes.TrimSpace(doc))
		if err == nil {
			return models.ComponentReference(cref), nil
		}
		log.Info("Not a plain uuid")
	}

	{
		// try component
		var cmp models.Component
		err := json.Unmarshal(doc, &cmp)
		if err == nil {
			return cmp, nil
		}
		log.Info("Not a component")
	}

	return models.ComponentReference{}, fmt.Errorf("could not parse '%s'", doc)
}

func main() {
	log.SetLevel(log.InfoLevel)

	fileName := flag.String("file", "", "Read from file instead of cmd line arg, '-' for stdin")
	dbName := flag.String("db", "Flowify", "Set the name of the database to use")
	flag.Parse()
	flag.Usage = myUsage

	// 1. read from arg (typically uid)
	// 2. read from file (if selected), - means stdin
	if (flag.NArg() == 1) == (*fileName != "") {
		flag.Usage()
		return
	}

	var bytes []byte

	if flag.NArg() == 1 {
		// 1. read from arg
		bytes = []byte(flag.Arg(0))
	} else if *fileName != "" {
		// 2. read from file

		var err error // nil error
		if *fileName == "-" {
			bytes, err = ioutil.ReadAll(bufio.NewReader(os.Stdin))
		} else {
			bytes, err = ioutil.ReadFile(*fileName)
		}
		if err != nil {
			panic(err)
		}
	} else {
		panic("unexpected")
	}

	any, err := parseInput(bytes)
	if err != nil {
		panic(err)
	}

	var component models.Component
	cstorage := storage.NewMongoStorageClient(storage.NewMongoClient(), *dbName)

	switch concrete := any.(type) {
	case models.ComponentReference:
		// retrieve
		c, err := cstorage.GetComponent(context.TODO(), concrete)
		if err != nil {
			fmt.Println("oops!")
			panic(err)
		}
		component = c
	case models.Component:
		component = concrete
	default:
		panic("unexpected")
	}

	cmpResolved, err := storage.DereferenceComponent(context.TODO(), cstorage, component)
	if err != nil {
		panic(err)
	}

	outBytes, _ := json.MarshalIndent(cmpResolved, "", "  ")
	fmt.Print(string(outBytes), "\n")
}
