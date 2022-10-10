package apiserver

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	argo_workflow "github.com/argoproj/argo-workflows/v3/pkg/client/clientset/versioned"
	gmux "github.com/gorilla/mux"
	"github.com/pkg/errors"

	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/rest"
	"github.com/equinor/flowify-workflows-server/storage"
	log "github.com/sirupsen/logrus"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

var CommitSHA = "unknown"
var BuildTime = "unknown"

type flowifyServer struct {
	k8Client      kubernetes.Interface
	namespace     string
	wfClient      argo_workflow.Interface
	nodeStorage   storage.ComponentClient
	volumeStorage storage.VolumeClient
	workspace     workspace.WorkspaceClient
	portnumber    int
	HttpServer    *http.Server
	auth          auth.AuthClient
}

func NewFlowifyServer(k8Client kubernetes.Interface,
	namespace string,
	wfclient argo_workflow.Interface,
	nodeStorage storage.ComponentClient,
	volumeStorage storage.VolumeClient,
	portnumber int,
	sec auth.AuthClient) (flowifyServer, error) {
	workspace := workspace.NewWorkspaceClient(k8Client, namespace)

	return flowifyServer{
		k8Client:      k8Client,
		namespace:     namespace,
		wfClient:      wfclient,
		nodeStorage:   nodeStorage,
		volumeStorage: volumeStorage,
		workspace:     workspace,
		portnumber:    portnumber,
		auth:          sec,
	}, nil
}

func (fs *flowifyServer) Run(ctx context.Context, readyNotifier *chan bool) {
	fs.HttpServer = fs.newHTTPServer(ctx, fs.portnumber)

	// Start listener
	var conn net.Listener
	var listerErr error
	address := fmt.Sprintf(":%d", fs.portnumber)

	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		conn, listerErr = net.Listen("tcp", address)

		if listerErr != nil {
			log.Warnf("failed to listen: %v", listerErr)
			return false, nil
		}
		return true, nil
	})

	defer conn.Close()

	if err != nil {
		log.Error(errors.Wrapf(err, "cannot create listener on socket %s", address))
		if readyNotifier != nil {
			// signal unsuccessful startup
			*readyNotifier <- false
		}
		panic("") // no return
	}

	go func() {
		err := fs.HttpServer.Serve(conn)
		switch err {
		case http.ErrServerClosed:
			log.Info("Server shutdown: ", err)
		default:
			log.Info("Server goroutine error: ", err)
		}
	}()
	log.WithFields(log.Fields{"version": CommitSHA, "port": address}).Info("✨ Flowify server started successfully ✨")

	if readyNotifier != nil {
		log.Info("Notify 'ready' channel")

		// signal successful startup
		*readyNotifier <- true

		// no more data will be sent here
		close(*readyNotifier)
	}

	// Handle graceful shutdown by relaying signals
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM)

	// Block until we receive SIGTERM.
	s := <-c
	log.Info("Signal: ", s)
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
	rest.RegisterRoutes(router.PathPrefix("/api/v1"), fs.nodeStorage, fs.volumeStorage, fs.wfClient, fs.k8Client, fs.auth, fs.workspace)

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

// newHTTPServer returns the HTTP server to serve HTTP/HTTPS requests. This is implemented
// using grpc-gateway as a proxy to the gRPC server.
func (fs *flowifyServer) newHTTPServer(ctx context.Context, port int) *http.Server {
	endpoint := fmt.Sprintf("localhost:%d", port)

	mux := gmux.NewRouter()
	mux.Use(LogRequestMiddleware)
	mux.Use(SetCustomHeaders)

	fs.registerApplicationRoutes(mux)

	return &http.Server{Addr: endpoint, Handler: mux}
}
