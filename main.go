package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/storage"

	"gopkg.in/yaml.v3"
)

const (
	maxWait = time.Second * 10
)

type KubernetesKonfig struct {
	KubeConfig string `mapstructure:"kubeconfig"`
	Namespace  string `mapstructure:"namespace"`
}

type LogConfig struct {
	LogLevel string `mapstructure:"loglevel"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type Config struct {
	DbConfig         storage.DbConfig `mapstructure:"db"`
	KubernetesKonfig KubernetesKonfig `mapstructure:"kubernetes"`
	AuthConfig       auth.AuthConfig  `mapstructure:"auth"`

	LogConfig    LogConfig    `mapstructure:"logging"`
	ServerConfig ServerConfig `mapstructure:"server"`
}

func LoadConfig(path string) (config Config, err error) {
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

	err = viper.Unmarshal(&config)
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
		cfg.KubernetesKonfig.KubeConfig = *kubeconfig
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

	// Kubernetes config
	k8sConfig, err := k8srest.InClusterConfig()
	if err != nil {
		log.Infof("No service account detected, running locally")

		k8sConfig, err = clientcmd.BuildConfigFromFlags("", cfg.KubernetesKonfig.KubeConfig)

		if err != nil {
			log.Errorf("Cannot load .kube/config from %v: %v", cfg.KubernetesKonfig.KubeConfig, err)
			return
		}
	}

	kubeclient := kubernetes.NewForConfigOrDie(k8sConfig)
	argoClient := wfclientset.NewForConfigOrDie(k8sConfig)

	// Auth config
	authClient, err := auth.ResolveAuthClient(cfg.AuthConfig)
	if err != nil {
		log.Fatalf("no auth handler set, %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := apiserver.NewFlowifyServer(kubeclient,
		cfg.KubernetesKonfig.Namespace,
		argoClient,
		storage.NewMongoStorageClient(storage.NewMongoClient(cfg.DbConfig), cfg.DbConfig.DbName),
		storage.NewMongoVolumeClient(storage.NewMongoClient(cfg.DbConfig), cfg.DbConfig.DbName),
		cfg.ServerConfig.Port,
		authClient,
	)

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
