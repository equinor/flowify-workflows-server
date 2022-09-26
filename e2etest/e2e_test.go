package test

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wf "github.com/argoproj/argo-workflows/v3/pkg/apiclient/workflow"
	wfv1 "github.com/argoproj/argo-workflows/v3/pkg/apis/workflow/v1alpha1"
	fuser "github.com/equinor/flowify-workflows-server/v2/user"
	"github.com/golang-jwt/jwt/v4"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type e2eTestSuite struct {
	suite.Suite
	client     *http.Client
	kubeclient *kubernetes.Clientset
}

var (
	auth_header = ""
	url         = "localhost:27017"
)

func init() {
	os.Setenv("FLOWIFY_K8S_NAMESPACE", "test")
}

func TestE2ETestSuite(t *testing.T) {
	suite.Run(t, &e2eTestSuite{})
}

func getKubeClient() *kubernetes.Clientset {
	config, err := rest.InClusterConfig()

	if err != nil {
		env := os.Getenv("KUBECONFIG")

		var path string

		if env == "" {
			usr, _ := user.Current()

			log.Infof("No service account detected, running locally")
			path = filepath.Join(usr.HomeDir, ".kube/config")
		} else {
			path = env
		}
		kubeconfig := flag.String("kubeconfig", path, "kubeconfig file")
		flag.Parse()

		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)

		if err != nil {
			log.Errorf("Cannot load kube config: %s", err)
			panic("Cannot load .kube file")
		}
	}

	return kubernetes.NewForConfigOrDie(config)
}

func (s *e2eTestSuite) SetupSuite() {
	s.client = &http.Client{}
	s.client.Timeout = time.Second * 30

	s.kubeclient = getKubeClient()

	mockUser := fuser.MockUser{Uid: "nonce", Name: "John Doe", Email: "user@test.com", Roles: []fuser.Role{"role-x", "role-y"}}
	jwtUser := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name":  mockUser.Name,
		"email": mockUser.Email,
		"roles": mockUser.Roles,
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Minute * 5).Unix(),
		"aud":   "e2e-test",
		"iss":   "e2e-test",
	})
	const secretKey = "my_secret_key"
	tokenString, err := jwtUser.SignedString([]byte(secretKey))
	require.NoError(s.T(), err)
	auth_header = tokenString

	opts := metav1.CreateOptions{}

	ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test"}}

	ns, err = s.kubeclient.CoreV1().Namespaces().Create(context.TODO(), ns, opts)
	s.NoError(err)
}

func (s *e2eTestSuite) TearDownSuite() {
	/*
		opts := metav1.DeleteOptions{}
		err := s.kubeclient.CoreV1().Namespaces().Delete(context.TODO(), "test", opts)
		s.NoError(err)
	*/
}

//
// === Actual integration test scenarios =======================================
//

const wf1 = `
{
  "serverDryRun": false,
  "createOptions": {},
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
          "workflows.argoproj.io/controller-instanceid": "my-instanceid",
      },
      "name": "hello-world-b6h5m",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "container": {
            "args": ["hello world"],
            "command": ["echo"],
            "image": "docker/whalesay:latest",
            "name": "",
            "resources": {}
          },
          "inputs": {},
          "metadata": {},
          "name": "whalesay",
          "outputs": {}
        }
      ]
    }
  }
}`
const wf2 = `{
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
        "owner": "role-x",
        "workflows.argoproj.io/phase": "Succeeded"
      },
      "name": "hello-world-9tql2-test",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "container": {
          "args": ["hello world"],
          "command": [
                    "cowsay"
                ],
          "image": "docker/whalesay:latest",
          "name": "",
          "resources": {}
          },
          "inputs": {},
          "metadata": {},
          "name": "whalesay",
          "outputs": {}
        }
      ]
    }
  }
}`

const wf3 = `{
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
        "workflows.argoproj.io/controller-instanceid": "my-instanceid",
        "owner": "role-y"
      },
      "name": "hello-world-b6h5m-test",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "container": {
            "args": ["hello world"],
            "command": ["cowsay"],
            "image": "docker/whalesay:latest",
            "name": "",
            "resources": {}
          },
          "inputs": {},
          "metadata": {},
          "name": "whalesay",
          "outputs": {}
        }
      ]
    }
  }
}`

const wf4 = `{
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
        "owner": "role-y",
      },
      "name": "wf4",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "name": "whalesay",
          "container": {
            "image": "docker/whalesay:latest",
            "command": [
              "cowsay"
            ],
            "args": [
              "hello world"
            ]
          }
        }
      ]
    }
  }
}
`

const wf5 = `{
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
        "owner": "role-y",
      },
      "name": "wf5",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "name": "whalesay",
          "container": {
            "image": "docker/whalesay:latest",
            "command": [
              "cowsay"
            ],
            "args": [
              "hello world"
            ]
          }
        }
      ]
    }
  }
}
`

// Incomplete: has no owner tag
const wf6 = `{
  "workflow": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "Workflow",
    "metadata": {
      "labels": {
        "owner": "role-y",
      },
      "name": "wf6",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "templates": [
        {
          "name": "whalesay",
          "container": {
            "image": "docker/whalesay:latest",
            "command": [
              "cowsay"
            ],
            "args": [
              "hello world"
            ]
          }
        }
      ]
    }
  }
}
`

const wft1 = `{
  "template": {
    "apiVersion": "argoproj.io/v1alpha1",
    "kind": "WorkflowTemplate",
    "metadata": {
      "labels": {
        "owner": "role-y"
      },
      "name": "wft1",
      "namespace": "test"
    },
    "spec": {
      "entrypoint": "whalesay",
      "workflowMetadata": {
        "labels": {
          "owner": "role-y"
        }
      },
      "templates": [
        {
          "container": {
            "args": ["hello world"],
            "command": ["echo"],
            "image": "docker/whalesay:latest",
            "name": "",
            "resources": {}
          },
          "inputs": {},
          "metadata": {},
          "name": "whalesay",
          "outputs": {}
        }
      ]
    }
  }
}`

func make_requestor(client *http.Client) func(string, string, string) (*http.Response, error) {
	return func(url, method string, payload string) (*http.Response, error) {
		return make_request_with_client(url, method, payload, client)
	}
}

type nameList struct {
	Names []string `json:"names"`
}

func make_request_with_client(url, method string, payload string, client *http.Client) (*http.Response, error) {
	req, _ := http.NewRequest(method, url, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth_header)

	return client.Do(req)
}

func (s *e2eTestSuite) Test_zpages() {
	resp, _ := http.Get("http://localhost:8842/versionz")

	s.Equal(http.StatusOK, resp.StatusCode)
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	stdout, _ := cmd.Output()

	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	git_sha := strings.TrimSuffix(string(stdout), "\n")
	s.Equal(git_sha, buf.String())
	s.Equal(git_sha, resp.Header.Get("X-Flowify-Version"))
	s.NotEqual(resp.Header.Get("X-Flowify-Buildtime"), "")
	s.Equal(resp.Header.Get("X-Wrong"), "")
}

func (s *e2eTestSuite) Test_Roundtrip_live_system() {
	requestor := make_requestor(s.client)

	var pp [7]string
	pp[0] = wf1
	pp[1] = wf2
	pp[2] = wf3

	for i := 0; i < 3; i++ {
		resp, err := requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, pp[i])
		s.NoError(err)

		if err != nil {
			s.T().Fatalf("Error reaching the flowify server: %v", err)
		}

		s.Equal(http.StatusOK, resp.StatusCode)
	}

	{
		type TestResponse struct {
			name     string
			response int
		}

		wf_list := []TestResponse{{"hello-world-b6h5m", http.StatusOK}, {"hello-world-9tql2-test", http.StatusOK}, {"hello-world-b6h5m-test", http.StatusOK}, {"hello-missing-workflow", http.StatusNotFound}}

		for _, testcase := range wf_list {
			resp, err := requestor("http://localhost:8842/api/v1/workflows/test/"+testcase.name, http.MethodGet, "")
			s.Equal(testcase.response, resp.StatusCode)
			s.NoError(err)
		}
	}
	resp, err := requestor("http://localhost:8842/api/v1/workflows/test/hello-world-9tql2-test", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflow-templates/test", http.MethodPost, wft1)
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/submit", http.MethodPost, `{"resourceKind": "WorkflowTemplate", "ResourceName": "wft1"}`)
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	buf_subm := new(bytes.Buffer)
	buf_subm.ReadFrom(resp.Body)

	var wf_subm wfv1.Workflow
	err = json.Unmarshal(buf_subm.Bytes(), &wf_subm)
	name_submitted := wf_subm.ObjectMeta.Name

	time.Sleep(3 * time.Second)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name_submitted+"/log?logOptions.container=main&logOptions.follow=true&logOptions.podName="+name_submitted,
		http.MethodGet, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)
	s.Equal("text/event-stream", resp.Header.Get("Content-Type"))

	buf_log := new(bytes.Buffer)

	// wait for up to 10s for the stream to deliver any body data
	for i := 0; i < 10; i++ {
		numRead, err := buf_log.ReadFrom(resp.Body)

		s.NoError(err)

		if numRead > 6 {
			break
		}
		time.Sleep(1 * time.Second)
	}

	buf_log.Next(6) // remove data prefix
	var objmap map[string]json.RawMessage

	err = json.Unmarshal(buf_log.Bytes(), &objmap)
	s.NoError(err)

	var entry wf.LogEntry
	s.NoError(json.Unmarshal(objmap["result"], &entry))
	s.Equal("hello world", entry.Content)

	var struk wfv1.WorkflowList
	{
		resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be {wf1, wf3, wft}

		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)

		err = json.Unmarshal(buf.Bytes(), &struk)

		s.Equal(3, len(struk.Items))
	}

	resp, err = requestor("http://localhost:8842/api/v1/workflow-events/test", http.MethodGet, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)
	s.NotNil(resp.Body)

	s.Equal("text/event-stream", resp.Header.Get("Content-Type"))

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/"+name_submitted, http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflow-templates/test/wft1", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, wf4) // wf4
	s.NoError(err)

	// Post in wrong namespace
	resp, err = requestor("http://localhost:8842/api/v1/workflows/test-no-access", http.MethodPost, wf1)
	s.NoError(err)
	s.Equal(http.StatusForbidden, resp.StatusCode)

	// Post in notexisting namespace
	resp, err = requestor("http://localhost:8842/api/v1/workflows/test-does-not-exist", http.MethodPost, wf2)
	s.NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	// --- Test creating without owner label -----------------------------------

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodPost, wf6) // wf 7 (use no owner)
	s.NoError(err)
	s.Equal(http.StatusBadRequest, resp.StatusCode)

	// --- Check if content is still as expected -------------------------------

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be 3 {wf1, wf3, wf4}
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	buf3 := new(bytes.Buffer)
	buf3.ReadFrom(resp.Body)
	json.Unmarshal(buf3.Bytes(), &struk)
	s.Equal(3, len(struk.Items))

	// --- Delete all resources, test delete of already removed resources-------

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m-test", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-9tql2-test", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/hello-world-b6h5m", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusNotFound, resp.StatusCode)

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test/wf4", http.MethodDelete, "")
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	// --- In the end there should be no workflows left ------------------------

	resp, err = requestor("http://localhost:8842/api/v1/workflows/test", http.MethodGet, "") // should be {}
	s.NoError(err)
	s.Equal(http.StatusOK, resp.StatusCode)

	var struk2 wfv1.WorkflowList
	buf4 := new(bytes.Buffer)
	buf4.ReadFrom(resp.Body)
	json.Unmarshal(buf4.Bytes(), &struk2)
	s.Equal(0, len(struk2.Items))
}

func marshalResponse(resp *http.Response, obj interface{}) error {
	buffer := new(bytes.Buffer)
	buffer.ReadFrom(resp.Body)

	return json.Unmarshal(buffer.Bytes(), obj)
}
