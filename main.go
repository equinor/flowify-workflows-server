package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mitchellh/mapstructure"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/equinor/flowify-workflows-server/apiserver"

	"gopkg.in/yaml.v3"
)

const (
	maxWait = time.Second * 10
)

func LoadConfig(path string) (config apiserver.Config, err error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	viper.AutomaticEnv() // let env override config if available

	// to allow environment parse nested config
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))

	// prefix all envs for uniqueness
	viper.SetEnvPrefix("FLOWIFY")

	err = viper.ReadInConfig()
	if err != nil {
		return
	}

	/*
		for _, k := range viper.AllKeys() {
			value := viper.GetString(k)
			log.Infoln(k, " : ", value)
		}
	*/

	f := viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			// Try to silent convert string to int
			// Port env var can be set as the string, not as required int
			func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
				if f.Kind() != reflect.String {
					return data, nil
				}
				if t.Kind() != reflect.Interface {
					return data, nil
				}
				v, err := strconv.Atoi(data.(string))
				if err != nil {
					return data, nil
				}
				return v, nil
			},
		),
	)

	err = viper.Unmarshal(&config, f)
	return
}

var status = 0

func logFatalHandler() {
	status = 1
	// send SIGTERM to itself to exit gracefully
	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	select {}
}

func isFlagPassed(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

func first[T any, S any](t T, s S) T { return t }

func main() {
	log.Infof("Starting process with pid %d", os.Getpid())
	log.RegisterExitHandler(logFatalHandler)

	// read config, possible overloaded by ENV VARS
	cfg, err := LoadConfig(".")
	if err != nil {
		log.Error("could not load config")
		return
	}

	// Set some common flags
	logLevel := flag.String("loglevel", "info", "Set the printout level for the logger (trace, debug, info, warn, error, fatal, panic)")
	portNumber := flag.Int("port", 8842, "Set the TCP port nubmer accepting connections")
	dbName := flag.String("db", "Flowify", "Set the name of the database to use")
	k8sConfigNamespace := flag.String("namespace", "test", "K8s configuration namespace to use")
	authHandlerSelector := flag.String("auth", "azure-oauth2-openid-token", "Set the security handler for the backend")
	kubeconfig := flag.String("kubeconfig", "~/kube/config", "path to kubeconfig file")
	dumpConfig := flag.String("dumpconfig", "", "Dump the config in yaml format to filename or stdout '-'")
	flag.Parse()

	// Connect flags to override config (flags > env > configfile )
	// viper nested keys dont work well with flags so do it explicitly: https://github.com/spf13/viper/issues/368
	if isFlagPassed("loglevel") {
		cfg.LogConfig.LogLevel = *logLevel
	}
	if isFlagPassed("port") {
		cfg.ServerConfig.Port = *portNumber
	}
	if isFlagPassed("db") {
		cfg.DbConfig.DbName = *dbName
	}
	if isFlagPassed("kubeconfig") {
		cfg.KubernetesKonfig.KubeConfigPath = *kubeconfig
	}
	if isFlagPassed("namespace") {
		cfg.KubernetesKonfig.Namespace = *k8sConfigNamespace
	}
	if isFlagPassed("auth") {
		cfg.AuthConfig.Handler = *authHandlerSelector
	}

	// handle config output
	if isFlagPassed("dumpconfig") {
		switch *dumpConfig {
		case "-":
			// stdout
			bytes, err := yaml.Marshal(viper.AllSettings())
			if err != nil {
				log.Error("Could not dump config", err)
				return
			}
			fmt.Println(string(bytes))
		default:
			viper.WriteConfigAs(*dumpConfig)
		}
	}

	// LogConfig is handled directly
	level, err := log.ParseLevel(cfg.LogConfig.LogLevel)
	if err != nil {
		log.Errorf("could not parse log level: %s", cfg.LogConfig)
	}
	log.SetLevel(level)
	log.WithFields(log.Fields{"Loglevel": log.StandardLogger().Level}).Infof("Setting global loglevel")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := apiserver.NewFlowifyServerFromConfig(cfg)
	if err != nil {
		panic("Cannot create a Flowify server object")
	}

	server.Run(ctx, nil)

	// Create a deadline to wait for.
	ctx, cancel = context.WithTimeout(context.Background(), maxWait)
	defer cancel()

	log.Info("Received SIGTERM: waiting for active requests to finish...")
	server.HttpServer.Shutdown(ctx)

	os.Exit(status)
}
