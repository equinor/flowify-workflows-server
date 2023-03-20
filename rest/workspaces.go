package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"net/http"

	"github.com/equinor/flowify-workflows-server/pkg/workspace"
	"github.com/equinor/flowify-workflows-server/user"
)

func RegisterWorkspaceRoutes(r *mux.Route, k8sclient kubernetes.Interface, namespace string) {
	s := r.Subrouter()

	const intype = "application/json"
	const outtype = "application/json"

	s.Use(CheckContentHeaderMiddleware(intype))
	s.Use(CheckAcceptRequestHeaderMiddleware(outtype))
	s.Use(SetContentTypeMiddleware(outtype))

	s.HandleFunc("/workspaces/", WorkspacesListHandler()).Methods(http.MethodGet)
	s.HandleFunc("/workspaces/", WorkspacesCreateHandler(k8sclient, namespace)).Methods(http.MethodPost)
}

func WorkspacesListHandler() http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wss := GetWorkspaceAccess(r.Context())
		lst := []workspace.WorkspaceGetRequest{}
		usr := user.GetUser(r.Context())
		for _, ws := range wss {
			wsgr := workspace.WorkspaceGetRequest{Name: ws.Name, Description: ws.Description}
			roles := []string{}
			if ws.UserHasAccess(usr) {
				roles = append(roles, "user")
			}
			if ws.UserHasAdminAccess(usr) {
				roles = append(roles, "admin")
			}
			wsgr.Roles = roles
			lst = append(lst, wsgr)
		}

		WriteResponse(w, http.StatusOK, nil, struct {
			Items []workspace.WorkspaceGetRequest `json:"items"`
		}{Items: lst}, "workspace")
	})
}

type WorkspaceCreateInputData struct {
	Name                string
	Roles               []string
	HideForUnauthorized string
	Labels              [][]string
}

func WorkspacesCreateHandler(k8sclient kubernetes.Interface, namespace string) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var creationData WorkspaceCreateInputData
		err := json.NewDecoder(r.Body).Decode(&creationData)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error decoding the input data: %v\n", err)}, "workspace")
		}

		labels := make(map[string]string)
		if creationData.Labels != nil {
			for _, label := range creationData.Labels {
				labels[label[0]] = label[len(label)-1]
			}
		}

		nsName := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   creationData.Name,
				Labels: labels,
			},
		}
		_, err = k8sclient.CoreV1().Namespaces().Create(context.Background(), nsName, metav1.CreateOptions{})
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("Error %s", err)}, "workspace")
		}

		var strBuffer bytes.Buffer
		strBuffer.WriteString("[")
		for _, role := range creationData.Roles {
			strBuffer.WriteString("\"")
			strBuffer.WriteString(role)
			strBuffer.WriteString("\", ")
		}
		roles := strBuffer.String()
		roles = roles[:len(roles)-2] + "]"

		cm := corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      creationData.Name,
				Namespace: namespace,
				Labels: map[string]string{
					"app.kubernetes.io/component": "workspace-config",
					"app.kubernetes.io/part-of":   "flowify",
				},
			},
			Data: map[string]string{
				"roles":               roles,
				"projectName":         creationData.Name,
				"hideForUnauthorized": creationData.HideForUnauthorized,
			},
		}

		CMOpt := metav1.CreateOptions{}
		_, err = k8sclient.CoreV1().ConfigMaps(namespace).Create(r.Context(), &cm, CMOpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating configMap: %v\n", err)}, "workspace")
		}

		//todo
		nstxt := "flowify"
		ROpt := metav1.CreateOptions{}
		rn := "flowify-server-" + creationData.Name + "-role"
		rules := []v1.PolicyRule{{
			APIGroups: []string{"*"},
			Resources: []string{"pods/log", "configmaps"},
			Verbs:     []string{"get", "list", "watch"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"pods"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"secrets"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"secret"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"serviceaccounts"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"roles", "rolebindings"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}}
		role := &v1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rn,
				Namespace: nstxt, // todo change to flowify
			},
			Rules: rules,
		}
		role1, err := k8sclient.RbacV1().Roles(nstxt).Create(context.Background(), role, ROpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating server Role: %v\n", err)}, "workspace")
			return
		}

		RBOpt := metav1.CreateOptions{}
		RBName := "flowify-server-" + creationData.Name + "-rolebinding"
		rr := v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role1.Name,
		}
		rb := &v1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      RBName,
				Namespace: nstxt,
			},
			RoleRef: rr,
			Subjects: []v1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "flowify-server",
				Namespace: nstxt, //todo change to flowify
			}},
		}
		_, err = k8sclient.RbacV1().RoleBindings(nstxt).Create(context.Background(), rb, RBOpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating server RoleBinding: %v\n", err)}, "workspace")
			return
		}

		ROpt = metav1.CreateOptions{}
		rn = creationData.Name + "-default-role"
		rules = []v1.PolicyRule{{
			APIGroups: []string{"argoproj.io"},
			Resources: []string{"workflows", "workflowtemplates", "cronworkflows"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"pods/log"},
			Verbs:     []string{"get", "list", "watch"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"configmaps"},
			Verbs:     []string{"get", "list"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"pods"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"secrets"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"secret"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"*"},
			Resources: []string{"serviceaccounts"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}, {
			APIGroups: []string{"rbac.authorization.k8s.io"},
			Resources: []string{"roles", "rolebindings"},
			Verbs:     []string{"create", "get", "list", "watch", "update", "patch", "delete"},
		}}
		role = &v1.Role{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Role",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      rn,
				Namespace: creationData.Name,
			},
			Rules: rules,
		}
		role2, err := k8sclient.RbacV1().Roles(creationData.Name).Create(context.Background(), role, ROpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating default Role: %v\n", err)}, "workspace")
			return
		}

		RBOpt = metav1.CreateOptions{}
		RBName = creationData.Name + "-default-rolebinding"
		rr = v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     role2.Name,
		}
		rb = &v1.RoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "RoleBinding",
				APIVersion: "rbac.authorization.k8s.io/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      RBName,
				Namespace: creationData.Name,
			},
			RoleRef: rr,
			Subjects: []v1.Subject{{
				Kind:      "ServiceAccount",
				Name:      "default",
				Namespace: creationData.Name,
			}},
		}
		_, err = k8sclient.RbacV1().RoleBindings(creationData.Name).Create(context.Background(), rb, RBOpt)
		if err != nil {
			WriteResponse(w, http.StatusInternalServerError, nil, struct {
				Error string
			}{Error: fmt.Sprintf("error creating default RoleBinding: %v\n", err)}, "workspace")
			return
		}

		WriteResponse(w, http.StatusCreated, nil, struct {
			Workspace string
		}{
			Workspace: fmt.Sprintf("The Workspace has been created %s", creationData.Name),
		}, "workspace")
	})
}
