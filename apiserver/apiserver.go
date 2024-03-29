package apiserver

import (
	"context"
	"fmt"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	k8srest "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	argo_workflow "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/pkg/secret"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/rest"
	"github.com/equinor/flowify-workflows-server/storage"
	gmux "github.com/gorilla/mux"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

var CommitSHA = "unknown"
var BuildTime = "unknown"

const ApiV1Path string = "/api/v1"

type flowifyServer struct {
	k8Client      kubernetes.Interface
	namespace     string
	wfClient      argo_workflow.Interface
	nodeStorage   storage.ComponentClient
	volumeStorage storage.VolumeClient
	workspace     workspace.WorkspaceClient
	secrets       secret.SecretClient
	portnumber    int
	HttpServer    *http.Server
	auth          auth.AuthenticationClient
	authz         auth.AuthorizationClient
}

func (f *flowifyServer) GetKubernetesClient() kubernetes.Interface {
	return f.k8Client
}

func (f *flowifyServer) GetAddress() string {
	return f.HttpServer.Addr
}

func NewFlowifyServerFromConfig(cfg Config) (flowifyServer, error) {

	// Kubernetes config
	k8sConfig, err := k8srest.InClusterConfig()
	if err != nil {
		log.Infof("No service account detected, running locally")

		k8sConfig, err = clientcmd.BuildConfigFromFlags("", cfg.KubernetesKonfig.KubeConfigPath)

		if err != nil {
			log.Errorf("Cannot load .kube/config from %v: %v", cfg.KubernetesKonfig.KubeConfigPath, err)
			return flowifyServer{}, errors.Wrap(err, "could not create ApiServer from config")
		}
	}

	kubeClient := kubernetes.NewForConfigOrDie(k8sConfig)
	argoClient := argo_workflow.NewForConfigOrDie(k8sConfig)

	nodeStorage, err := storage.NewMongoStorageClientFromConfig(cfg.DbConfig, nil)
	if err != nil {
		return flowifyServer{}, errors.Wrap(err, "could not create new node storage")
	}

	volumeStorage, err := storage.NewMongoVolumeClientFromConfig(cfg.DbConfig, nil)
	if err != nil {
		return flowifyServer{}, errors.Wrap(err, "could not create new volume storage")
	}

	workspaceClient := workspace.NewWorkspaceClient(kubeClient, cfg.KubernetesKonfig.Namespace)
	secretClient := secret.NewSecretClient(kubeClient)

	authClient, err := auth.NewAuthClientFromConfig(cfg.AuthConfig)
	if err != nil {
		return flowifyServer{}, errors.Wrap(err, "could not create auth")
	}

	authz := auth.RoleAuthorizer{Workspaces: workspaceClient}

	return flowifyServer{
		k8Client:      kubeClient,
		namespace:     cfg.KubernetesKonfig.Namespace,
		wfClient:      argoClient,
		nodeStorage:   nodeStorage,
		volumeStorage: volumeStorage,
		workspace:     workspaceClient,
		secrets:       secretClient,
		portnumber:    cfg.ServerConfig.Port,
		auth:          authClient,
		authz:         authz,
	}, nil
}

func NewFlowifyServer(k8Client kubernetes.Interface,
	namespace string,
	wfclient argo_workflow.Interface,
	nodeStorage storage.ComponentClient,
	volumeStorage storage.VolumeClient,
	portnumber int,
	sec auth.AuthenticationClient) (flowifyServer, error) {
	workspace := workspace.NewWorkspaceClient(k8Client, namespace)
	secretClient := secret.NewSecretClient(k8Client)
	authz := auth.RoleAuthorizer{Workspaces: workspace}

	return flowifyServer{
		k8Client:      k8Client,
		namespace:     namespace,
		wfClient:      wfclient,
		nodeStorage:   nodeStorage,
		volumeStorage: volumeStorage,
		workspace:     workspace,
		secrets:       secretClient,
		portnumber:    portnumber,
		auth:          sec,
		authz:         authz,
	}, nil
}

func (fs *flowifyServer) Run(ctx context.Context, readyNotifier *chan bool) error {
	fs.HttpServer = fs.newHTTPServer(ctx, fs.portnumber)

	// Start listener
	var conn net.Listener
	var listerErr error
	address := fmt.Sprintf(":%d", fs.portnumber)

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		conn, listerErr = net.Listen("tcp", address)

		if listerErr != nil {
			log.Warnf("failed to listen at addr=%v. %v", address, listerErr)
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		if readyNotifier != nil {
			// signal unsuccessful startup
			*readyNotifier <- false
		}
		// signal service failure
		return errors.Wrap(err, "server run failure")
	}

	go func() {
		// defer close in this goroutine to make sure Connection lifespan matches usage
		defer conn.Close()

		err := fs.HttpServer.Serve(conn)
		switch err {
		case http.ErrServerClosed:
			log.Info("Server shutdown: ", err)
		default:
			log.Info("Server goroutine error: ", err)
		}
	}()
	log.WithFields(log.Fields{"version": CommitSHA, "buildtime": BuildTime, "port": address}).Info("✨ Flowify server started successfully ✨")

	if readyNotifier != nil {
		log.Info("Notify 'ready' channel")

		// signal successful startup
		*readyNotifier <- true

		// no more data will be sent here
		close(*readyNotifier)
	}

	// Handle graceful shutdown by relaying signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)

	// Block until we receive signals.
	s := <-c
	log.Info("Signal: ", s)

	return nil
}

func logHTTPRequest(r *http.Request, start time.Time, ignoreList []string) {
	for _, item := range ignoreList {
		if r.URL.Path == item {
			return
		}
	}

	origin, _, _ := net.SplitHostPort(r.RemoteAddr)

	if origin == "::1" || origin == "127.0.0.1" {
		origin = "localhost"
	}

	log.Infof("origin: %s\trequest: %s %s %s\tspan: %s", origin, r.Method, r.URL, r.Proto, time.Since(start))
}

func LogRequestMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)

		// Log request after it has been completed so we can log handling duration
		logHTTPRequest(r, start, []string{"/readyz", "/livez", "/versionz"})
	})
}

func SetCustomHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Flowify-Version", CommitSHA)
		w.Header().Set("X-Flowify-Buildtime", BuildTime)

		next.ServeHTTP(w, r)
	})
}

func (fs *flowifyServer) registerApplicationRoutes(router *gmux.Router) {
	// send a pathprefix that catches all and handle in a subrouter to avoid interference
	rest.RegisterRoutes(router.PathPrefix(ApiV1Path), fs.nodeStorage, fs.volumeStorage, fs.secrets, fs.wfClient, fs.k8Client, fs.auth, fs.authz, fs.workspace, fs.namespace)

	router.HandleFunc("/livez", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "alive") }).Methods(http.MethodGet)
	router.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, "ready") }).Methods(http.MethodGet)
	router.HandleFunc("/versionz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, CommitSHA)
	}).Methods(http.MethodGet)

	// output the handlers
	router.Walk(func(route *gmux.Route, router *gmux.Router, ancestors []*gmux.Route) error {
		path, _ := route.GetPathTemplate()
		method, _ := route.GetMethods()
		if method == nil {
			return nil
		}
		fmt.Println("Com: ", path, method)
		return nil
	})
}

func (fs *flowifyServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	endpoint := fmt.Sprintf("localhost:%d", port)

	mux := gmux.NewRouter()
	mux.Use(LogRequestMiddleware)
	mux.Use(SetCustomHeaders)

	fs.registerApplicationRoutes(mux)

	return &http.Server{Addr: endpoint, Handler: mux}
}
