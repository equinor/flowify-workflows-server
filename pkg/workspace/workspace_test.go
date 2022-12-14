package workspace_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/stretchr/testify/require"
	core "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	namespace = "dummy-namespace"
)

const ConfigMap1 = `{
	"apiVersion": "v1",
	"kind": "ConfigMap",
	"metadata": {
	  "labels": {
		"app.kubernetes.io/component": "workspace-config",
		"app.kubernetes.io/part-of": "flowify"
	  },
	  "name": "workspace-abc",
	  "namespace": "dummy-namespace"
	},
	"data": {
	  "roles": "[[\"token1\", \"token2\", \"token3\"], [\"token4\"]]",
	  "projectName": "workspace-abc",
	  "hideForUnauthorized": "true"
	}
  }
  `

const ConfigMap2 = `{
	"apiVersion": "v1",
	"kind": "ConfigMap",
	"metadata": {
	  "labels": {
		"app.kubernetes.io/component": "workspace-config",
		"app.kubernetes.io/part-of": "flowify"
	  },
	  "name": "workspace-xyz",
	  "namespace": "dummy-namespace"
	},
	"data": {
	  "roles": "[\"token1\", \"token4\"]",
	  "projectName": "workspace-xyz",
	  "hideForUnauthorized": "false"
	}
  }
  `

const ConfigMap3 = `{
	"apiVersion": "v1",
	"kind": "ConfigMap",
	"metadata": {
	  "labels": {
		"app.kubernetes.io/component": "workspace-config",
		"app.kubernetes.io/part-of": "flowify"
	  },
	  "name": "test-workspace",
	  "namespace": "flowify"
	},
	"data": {
	  "roles": "[\"role1\", \"role3\"]",
	  "projectName": "test-workspace",
	  "hideForUnauthorized": "true",
	  "serviceAccountName": "default"
	}
  }
  `

const WorkspaceDescriptions = `{
	"apiVersion": "v1",
	"kind": "ConfigMap",
	"metadata": {
	  "labels": {
		"app.kubernetes.io/part-of": "flowify"
	  },
	  "name": "role-descriptions",
	  "namespace": "dummy-namespace"
	},
	"data": {
	  "token1": "Need superpowers",
	  "token2": "This is handed out freely",
	  "token3": "Complain to your boss",
	  "token4": "Only given to the bravest",
	  "token5": "Nobody knows how to get this"
	}
  }
  `

var (
	ctx = context.TODO()
)

func init() {

}

func getClient() workspace.WorkspaceClient {
	var cm1, cm2, descriptions core.ConfigMap

	json.Unmarshal([]byte(ConfigMap1), &cm1)
	json.Unmarshal([]byte(ConfigMap2), &cm2)
	json.Unmarshal([]byte(WorkspaceDescriptions), &descriptions)

	clientSet := fake.NewSimpleClientset(&cm1, &cm2, &descriptions)

	return workspace.NewWorkspaceClient(clientSet, namespace)
}

func Test_WorkspaceClientListWorkspaces(t *testing.T) {
	client := getClient()
	ws := client.ListWorkspaces()

	// Should return both workspaces
	require.Len(t, ws, 2)
	for _, w := range ws {
		require.Contains(t, []string{"workspace-xyz", "workspace-abc"}, w.Name)
	}
}

func Test_WorkspaceNoRoleConfigMap(t *testing.T) {
	var cm1, cm2 core.ConfigMap

	json.Unmarshal([]byte(ConfigMap1), &cm1)
	json.Unmarshal([]byte(ConfigMap2), &cm2)

	client := workspace.NewWorkspaceClient(fake.NewSimpleClientset(&cm1, &cm2), namespace)

	ws := client.ListWorkspaces()

	// Should return both workspaces
	require.Len(t, ws, 2)
	for _, w := range ws {
		require.Contains(t, []string{"workspace-xyz", "workspace-abc"}, w.Name)
	}
}
