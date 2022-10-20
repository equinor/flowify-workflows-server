package main

import (
	"context"
	"flag"
	"os"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/equinor/flowify-workflows-server/apiserver"
)

const (
	maxWait = time.Second * 10
)

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

//func first[T any, S any](t T, s S) T { return t }

func main() {
	log.Infof("Starting process with pid %d", os.Getpid())
	log.RegisterExitHandler(logFatalHandler)

	// read config, possible overloaded by ENV VARS
	cfg, err := apiserver.LoadConfigFromPath(".", apiserver.LoadOptions{DenyEnvironmentOverride: false})
	if err != nil {
		log.Error("could not load config, ", err)
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
		cfg.Dump(*dumpConfig)
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
		log.Error("Cannot create a Flowify server object", err)
		os.Exit(1)
	}

	// run is a blocking call, but may return early on error
	err = server.Run(ctx, nil)
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}

	// Create a deadline to wait for.
	ctx, cancel = context.WithTimeout(context.Background(), maxWait)
	defer cancel()

	log.Info("Received SIGNAL: waiting for active requests to finish...")
	server.HttpServer.Shutdown(ctx)

	os.Exit(status)
}
