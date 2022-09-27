package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	osuser "os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/MicahParks/keyfunc"
	wfclientset "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/equinor/flowify-workflows-server/apiserver"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/equinor/flowify-workflows-server/user"
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

func resolveAuthClient(spec *string) (auth.AuthClient, error) {
	if *spec == "" {
		return nil, fmt.Errorf("no auth handler selected")
	}

	switch *spec {
	case "azure-oauth2-openid-token":
		{
			const (
				JWT_ISSUER_CLAIM = "TENANT_ID"
				JWT_AUD_CLAIM    = "CLIENT_ID"
				JWT_KEYS_URL     = "JWKS_URI"
			)

			iss, ok := os.LookupEnv(JWT_ISSUER_CLAIM)
			if !ok {
				return nil, fmt.Errorf("env %s missing", JWT_ISSUER_CLAIM)
			}

			aud, ok := os.LookupEnv(JWT_AUD_CLAIM)
			if !ok {
				return nil, fmt.Errorf("env %s missing", JWT_AUD_CLAIM)
			}

			kUrl, ok := os.LookupEnv(JWT_KEYS_URL)
			if !ok {
				return nil, fmt.Errorf("env %s missing", JWT_KEYS_URL)
			}

			opts := auth.AzureTokenAuthenticatorOptions{}
			var jwks auth.AzureKeyFunc
			if kUrl == "DISABLE_JWT_SIGNATURE_VERIFICATION" {
				log.Warn("running the authenticator without signature verification is UNSAFE")
				opts.DisableVerification = true
			} else {
				// Create the JWKS from the resource at the given URL.
				JWKS, err := keyfunc.Get(kUrl, keyfunc.Options{
					// best practices for azure key roll-over: https://docs.microsoft.com/en-us/azure/active-directory/develop/active-directory-signing-key-rollover
					RefreshInterval:  time.Hour * 24,
					RefreshRateLimit: time.Minute * 5,
					// when encountering a "new" key id, allow immediate refresh (rate limited)
					RefreshUnknownKID: true,
					// make sure errors make it into the log
					RefreshErrorHandler: func(err error) { log.Error("jwks refresh error:", err) },
				})
				if err != nil {
					return nil, errors.Wrap(err, "failed to get the JWKS")
				}
				jwks = JWKS.Keyfunc
			}

			return auth.AzureTokenAuthenticator{Issuer: iss, Audience: aud, KeyFunc: jwks, Options: opts}, nil
		}

	case "disabled-auth":
		{
			log.Warn("flowify started with no authentication and static dummy-authorization")
			return auth.MockAuthenticator{
				User: user.MockUser{
					Uid:   "0",
					Name:  "Auth Disabled",
					Email: "auth@disabled",
					Roles: []user.Role{"tester", "dummy"},
				},
			}, nil
		}
	default:
		{
			return nil, fmt.Errorf("auth handler (%s) not supported", *spec)
		}
	}
}

func main() {
	log.Infof("Starting process with pid %d", os.Getpid())
	log.RegisterExitHandler(logFatalHandler)

	logLevel := flag.Int("v", 4 /* Info */, "Set the printout level for the logger (0 -- 6)")
	portNumber := flag.Int("p", 8842 /* Info */, "Set the TCP port nubmer accepting connections")
	dbName := flag.String("db", "Flowify", "Set the name of the database to use")
	authHandlerSelector := flag.String("flowify-auth", "azure-oauth2-openid-token", "Set the security handler for the backend")

	path, err := findKubeConfig()
	if err != nil {
		log.Info("No local kubeconfig setup detected")
		path = ""
	}
	kubeconfig := flag.String("kubeconfig", path, "kubeconfig file")
	flag.Parse()

	log.SetLevel(log.Level(*logLevel))
	log.WithFields(log.Fields{"Loglevel": log.StandardLogger().Level}).Infof("setting loglevel")

	config, err := k8srest.InClusterConfig()

	if err != nil {
		log.Infof("No service account detected, running locally")

		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)

		if err != nil {
			log.Errorf("Cannot load .kube/config file")
			return
		}
	}

	kubeclient := kubernetes.NewForConfigOrDie(config)
	wfclient := wfclientset.NewForConfigOrDie(config)

	authClient, err := resolveAuthClient(authHandlerSelector)
	if err != nil {
		log.Fatalf("no auth handler set, %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := apiserver.NewFlowifyServer(kubeclient,
		wfclient,
		storage.NewMongoStorageClient(storage.NewMongoClient(), *dbName),
		storage.NewMongoVolumeClient(storage.NewMongoClient(), *dbName),
		*portNumber,
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

func findKubeConfig() (string, error) {
	env := os.Getenv("KUBECONFIG")

	if env != "" {
		log.Info(fmt.Sprintf("using environment var KUBECONFIG (%s) to locate .kube/config", env))
		return env, nil
	}

	usr, err := osuser.Current()
	if err != nil {
		log.Error("no current user found when looking for .kube/config")
		return "", err
	}

	log.Info(fmt.Sprintf("Using current user home dir '%s' to locate .kube/config", usr.HomeDir))

	return filepath.Join(usr.HomeDir, ".kube/config"), nil
}
