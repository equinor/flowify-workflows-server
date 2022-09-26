package secret

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
)

var backoff = wait.Backoff{
	Steps:    5,
	Duration: 500 * time.Millisecond,
	Factor:   1.0,
	Jitter:   0.1,
}

const (
	DefaultObjectName = "flowify-default"
)

type SecretClient interface {
	ListAvailableKeys(ctx context.Context, group string) ([]string, error)
	AddSecretKey(ctx context.Context, group, name, key string) error
	DeleteSecretKey(ctx context.Context, group, name string) error
}

// Implements secret.SecretClient
type SecretClientImpl struct {
	clientSet kubernetes.Interface
}

func NewSecretClient(clientSet kubernetes.Interface) SecretClient {
	return &SecretClientImpl{clientSet: clientSet}
}

func (c *SecretClientImpl) AddSecretKey(ctx context.Context, workspace, key, value string) error {
	secret_exists, err := k8sSecretExists(ctx, c.clientSet, workspace)

	if err != nil {
		log.Warning("Error reaching k8s server: " + err.Error())
		return err
	}

	if !secret_exists {
		log.WithFields(log.Fields{"secret": workspace}).Info("Creating new secret")
		err := c.createNewGroup(ctx, workspace)

		if err != nil {
			return errors.Wrap(err, "cannot create new secret group")
		}
	}

	return c.addFieldToSecret(ctx, workspace, key, value)
}

func k8sSecretExists(ctx context.Context, clientSet kubernetes.Interface, workspace string) (bool, error) {
	_, err := clientSet.CoreV1().Secrets(workspace).Get(ctx, DefaultObjectName, metav1.GetOptions{})

	if err != nil {
		if err.(*k8serrors.StatusError).Status().Reason == metav1.StatusReasonNotFound {
			return false, nil
		} else {
			return false, errors.Wrap(err, "cannot get secret")
		}
	}

	return true, nil
}

func (c *SecretClientImpl) ListAvailableKeys(ctx context.Context, workspace string) ([]string, error) {
	secret, err := c.clientSet.CoreV1().Secrets(workspace).Get(ctx, DefaultObjectName, metav1.GetOptions{})

	if err != nil {
		if k8serrors.IsNotFound(err) {
			// default-secret not yet created, just return an empty list for the keys, secret-initialization is handled when adding keys
			log.Warnf("Trying to list keys from non-existing secret in namespace %s, return empty key-list", workspace)
			return []string{}, nil
		} else {
			return []string{}, errors.Wrap(err, "cannot get secret list")
		}
	}

	keys := make([]string, len(secret.Data))

	i := 0
	for key := range secret.Data {
		keys[i] = key
		i++
	}

	return keys, nil
}

func (c *SecretClientImpl) DeleteSecretKey(ctx context.Context, workspace, name string) error {
	patch := fmt.Sprintf(`[
        { "op": "remove", "path": "/data/%s" }
    ]`, name)

	_, err := c.clientSet.CoreV1().Secrets(workspace).Patch(ctx, DefaultObjectName, types.JSONPatchType, []byte(patch), metav1.PatchOptions{})

	return err
}

func (c *SecretClientImpl) addFieldToSecret(ctx context.Context, workspace, key, value string) error {
	// etcd uses optimistic concurrency control/locking: if the update fails
	// because the remote version was updated between the read-write cycle, it
	// is the responsibility of the client to re-initiate the cycle and try
	// again. This usually works fine in k8s as the resource update frequency is
	// typically low enough.
	err := wait.ExponentialBackoff(backoff, func() (bool, error) {
		secret, err := c.clientSet.CoreV1().Secrets(workspace).Get(ctx, DefaultObjectName, metav1.GetOptions{})

		if err != nil {
			return false, errors.Wrapf(err, "cannot fetch secrets from %s", workspace)
		}

		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}

		secret.Data[key] = []byte(value)
		_, err = c.clientSet.CoreV1().Secrets(workspace).Update(ctx, secret, metav1.UpdateOptions{})

		if err != nil {
			return false, errors.Wrapf(err, "failed to update secret %s with key %s", workspace, key)
		}

		return true, nil
	})

	return err
}

func (c *SecretClientImpl) createNewGroup(ctx context.Context, workspaceName string) error {
	var err error

	_, err = addNewRole(ctx, c.clientSet, workspaceName)

	if err != nil {
		return errors.Wrap(err, "cannot add rbac.authorization.k8s.io/v1/Role")
	}

	_, err = addNewRoleBinding(ctx, c.clientSet, workspaceName)

	if err != nil {
		return errors.Wrap(err, "cannot add new rbac.authorization.k8s.io/v1/Rolebinding")
	}

	_, err = addNewSecret(ctx, c.clientSet, workspaceName)

	if err != nil {
		return errors.Wrap(err, "cannot add new core/v1/Secret")
	}

	_, err = addNewServiceAccount(ctx, c.clientSet, workspaceName)

	if err != nil {
		return errors.Wrap(err, "cannot add new core/v1/ServiceAccount")
	}

	return nil
}

func addNewServiceAccount(ctx context.Context, cl kubernetes.Interface, namespace string) (*core.ServiceAccount, error) {
	opts := metav1.CreateOptions{}
	sa := &core.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: DefaultObjectName}}

	return cl.CoreV1().ServiceAccounts(namespace).Create(ctx, sa, opts)
}

func addNewRole(ctx context.Context, cl kubernetes.Interface, namespace string) (*rbac.Role, error) {
	opts := metav1.CreateOptions{}
	role := &rbac.Role{ObjectMeta: metav1.ObjectMeta{Name: DefaultObjectName},
		Rules: []rbac.PolicyRule{
			{
				Verbs:         []string{"get"},
				APIGroups:     []string{""},
				Resources:     []string{"secret"},
				ResourceNames: []string{DefaultObjectName}},
			{
				Verbs:     []string{"create"},
				APIGroups: []string{""},
				Resources: []string{"pods"}},
			{
				Verbs:     []string{"create"},
				APIGroups: []string{"argoproj.io"},
				Resources: []string{"workflows"}},
		}}

	return cl.RbacV1().Roles(namespace).Create(ctx, role, opts)
}

func addNewRoleBinding(ctx context.Context, cl kubernetes.Interface, namespace string) (*rbac.RoleBinding, error) {
	opts := metav1.CreateOptions{}
	rolebinding := &rbac.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: DefaultObjectName},
		Subjects: []rbac.Subject{{
			Kind:      "ServiceAccount",
			Name:      DefaultObjectName,
			Namespace: namespace}},
		RoleRef: rbac.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     DefaultObjectName}}
	return cl.RbacV1().RoleBindings(namespace).Create(ctx, rolebinding, opts)
}

func addNewSecret(ctx context.Context, cl kubernetes.Interface, namespace string) (*core.Secret, error) {
	const secretType = "Opaque"

	opts := metav1.CreateOptions{}
	secret := &core.Secret{ObjectMeta: metav1.ObjectMeta{Name: DefaultObjectName}, Type: core.SecretType(secretType)}

	return cl.CoreV1().Secrets(namespace).Create(ctx, secret, opts)
}
