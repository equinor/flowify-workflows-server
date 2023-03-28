package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	apicore1 "k8s.io/api/core/v1"
	core "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	v1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"strings"
	"sync"
	"time"

	userpkg "github.com/equinor/flowify-workflows-server/user"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	RolesKey                        = "roles"
	IsHiddenKey                     = "hideForUnauthorized"
	DefaultInformerResync           = time.Second * 10
	roleDescriptionConfigMapName    = "role-descriptions"
	workspaceConfigMapLabel         = "app.kubernetes.io/component"
	workspaceConfigMapLabelSelector = "workspace-config"
)

type CreationData struct {
	Name                string
	Roles               []string
	HideForUnauthorized string
	Labels              map[string]string
	Namespace           string
}

type CreateInputData struct {
	Name                string
	Roles               []string
	HideForUnauthorized string
	Labels              [][]string
}

type Workspace struct {
	Name                string `json:"name"`
	Description         string `json:"description"`
	HideForUnauthorized bool   `json:"hideForUnauthorized"`

	// the list of required roles for access
	Roles [][]userpkg.Role `json:"roles,omitempty"`
}

type WorkspaceClient interface {
	// list the workspaces visible to a specific user
	ListWorkspaces() []Workspace
	GetNamespace() string
	Create(k8sclient kubernetes.Interface, cd CreationData) (string, error)
}

func NewWorkspaceClient(clientSet kubernetes.Interface, namespace string) WorkspaceClient {
	informerFactory := informers.NewSharedInformerFactoryWithOptions(clientSet, DefaultInformerResync, informers.WithNamespace(namespace))
	cmInformer := informerFactory.Core().V1().ConfigMaps()

	obj := &workspaceImpl{
		clientSet:  clientSet,
		cmInformer: cmInformer,
		namespace:  namespace,
	}

	cmInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(cm interface{}) {
				obj.Update()
			},
			DeleteFunc: func(cm interface{}) { obj.Update() },
			UpdateFunc: func(cm interface{}, cmNew interface{}) {
				if cm.(*core.ConfigMap).ResourceVersion == cmNew.(*core.ConfigMap).ResourceVersion {
					return
				}

				if cmNew.(*core.ConfigMap).Labels["app.kubernetes.io/component"] == "workspace-config" {
					obj.Update()
				}
			},
		})

	// Start the informers
	informerFactory.Start(wait.NeverStop)
	informerFactory.WaitForCacheSync(wait.NeverStop)

	// populate the initial ws-list
	obj.Update()

	return obj
}

type workspaceImpl struct {
	clientSet  kubernetes.Interface
	cmInformer v1.ConfigMapInformer
	namespace  string

	// hold a list of workspaces
	// updated async on push notifications
	ws []Workspace

	// the corresponding role descriptions
	roleDescriptions map[string]string

	// for safely updating the ws list concurrently
	mutex sync.Mutex
}

type Capability = int

const (
	None Capability = iota
	Read
	Write
	Delete
	Execute
)

type ContextKey int

const (
	// the key type to use when adding WorkspaceAccess to a Context
	WorkspaceKey ContextKey = iota
)

type MissingRole struct {
	Name        userpkg.Role `json:"name"`
	Description string       `json:"description"`
}

func listWorkspaceConfigMaps(namespace string, cmInformer v1.ConfigMapInformer) ([]Workspace, error) {
	// the lister finds all configmaps in the given namespace with the 'workspace-config' label

	configMaps, err := cmInformer.Lister().ConfigMaps(namespace).List(
		labels.SelectorFromSet(map[string]string{workspaceConfigMapLabel: workspaceConfigMapLabelSelector}))

	if err != nil {
		logrus.Warn("could not list workspaces with informer", err)
	}

	newlist := []Workspace{}
	for _, cm := range configMaps {

		roles, err := getAccessTokens(cm)
		if err != nil {
			logrus.Warnf("bad configmap detected: %s.%s. skipping", cm.Namespace, cm.Name)
			continue
		}

		var hideForUnauthorized bool
		json.Unmarshal([]byte(cm.Data[IsHiddenKey]), &hideForUnauthorized)

		ws := Workspace{
			Name:                cm.Name,
			Roles:               roles,
			HideForUnauthorized: hideForUnauthorized,
			Description:         cm.Data["description"],
		}
		newlist = append(newlist, ws)
	}
	return newlist, nil
}

func (w *workspaceImpl) Update() {
	// get the configmaps with workspaces from k8s
	w.mutex.Lock()
	defer w.mutex.Unlock()

	ws, err := listWorkspaceConfigMaps(w.namespace, w.cmInformer)
	if err != nil {
		logrus.WithField("error", err).Error("configmaps update failed")
		return
	}

	roleDescCM, err := w.cmInformer.Lister().ConfigMaps(w.namespace).Get(roleDescriptionConfigMapName)
	if err != nil && k8serrors.IsNotFound(err) {
		logrus.Warnf("role descriptions configmap (%s:%s) not found", w.namespace, roleDescriptionConfigMapName)
		// not a hard error to not have the descriptions cm
	} else if err != nil {
		logrus.Warnf("role descriptions configmap (%s:%s) update failed: %v", w.namespace, roleDescriptionConfigMapName, err)
		return
	}

	// assign updated data
	w.ws = ws
	if roleDescCM != nil {
		w.roleDescriptions = roleDescCM.DeepCopy().Data
	}
}

func (w *workspaceImpl) GetNamespace() string {
	return w.namespace
}

func (ws Workspace) UserHasAccess(user userpkg.User) bool {
	for _, rs := range ws.Roles {
		// rs is a list of roles of which the user has to fulfill all of to gain access
		var userHasRole bool
		for _, r := range rs {
			userHasRole = userpkg.UserHasRole(user, r)
			if !userHasRole {
				// missing a role, stop investigating current list
				break
			}
		}

		if !userHasRole {
			// missing at least one role in current list, continue with next list
			continue
		}
		// passed a complete list of necessary roles, success
		return true
	}
	return false
}

func AdminRole(userrole userpkg.Role) userpkg.Role {
	const adminSuffix string = "-admin"
	return userpkg.Role(userrole + userpkg.Role(adminSuffix))
}

func (ws Workspace) UserHasAdminAccess(user userpkg.User) bool {
	for _, rs := range ws.Roles {
		// rs is a list of roles of which the user has to fulfill all of to gain access
		var userHasRole bool
		for _, r := range rs {
			userHasRole = userpkg.UserHasRole(user, AdminRole(r))
			if !userHasRole {
				// missing a role, stop investigating current list
				break
			}
		}

		if !userHasRole {
			// missing at least one role in current list, continue with next list
			continue
		}
		// passed a complete list of necessary roles, success
		return true
	}
	return false
}

func getAccessTokens(cm *core.ConfigMap) ([][]userpkg.Role, error) {
	roleString := cm.Data[RolesKey]

	if strings.Count(roleString, "[") > 1 { // array of arrays
		var requiredTokens [][]userpkg.Role
		err := json.Unmarshal([]byte(roleString), &requiredTokens)

		if err != nil {
			return nil, errors.Wrap(err, "cannot unmarshal token array of arrays")
		}

		return requiredTokens, err
	} else {
		var requiredTokens [][]userpkg.Role
		var tokens []userpkg.Role
		err := json.Unmarshal([]byte(roleString), &tokens)

		if err != nil {
			return nil, errors.Wrap(err, "cannot unmarshal token array of arrays")
		}

		return append(requiredTokens, tokens), nil
	}
}

func (w *workspaceImpl) Create(k8sclient kubernetes.Interface, creationData CreationData) (string, error) {

	nsName := &apicore1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   creationData.Name,
			Labels: creationData.Labels,
		},
	}
	_, err := k8sclient.CoreV1().Namespaces().Create(context.Background(), nsName, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("error %s", err)
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

	cm := apicore1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      creationData.Name,
			Namespace: creationData.Namespace,
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
	_, err = k8sclient.CoreV1().ConfigMaps(creationData.Namespace).Create(context.Background(), &cm, CMOpt)
	if err != nil {
		return "", fmt.Errorf("error %s", err)
	}
	return fmt.Sprintf("The workspace %s has been created", nsName.Name), nil
}

func (wimpl *workspaceImpl) ListWorkspaces() []Workspace {
	return wimpl.ws
}

type WorkspaceGetRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Roles       []string `json:"roles"`
}
