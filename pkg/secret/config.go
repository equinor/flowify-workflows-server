package secret

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	DefaultServiceAccountName = "default"
	AccountKey                = "account"
	cmKeyServiceAccount       = "serviceAccountName"
)

func InClusterConfig(ctx context.Context, client kubernetes.Interface, namespace, workspace string) (*rest.Config, error) {
	sa, err := lookupServiceAccount(ctx, client, workspace, namespace)

	if err != nil {
		return nil, errors.Wrap(err, "cannot look-up service account")
	}

	if len(sa.Secrets) == 0 {
		return nil, errors.New(fmt.Sprintf("service account %s has no attached secret", workspace))
	}

	secret, err := client.CoreV1().Secrets(workspace).Get(ctx, sa.Secrets[0].Name, metav1.GetOptions{})

	if err != nil {
		return nil, errors.Wrapf(err, "cannot get secret %s/%s", workspace, sa.Secrets[0].Name)
	}

	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, rest.ErrNotInCluster
	}

	tlsClientConfig := rest.TLSClientConfig{}
	tlsClientConfig.CAData = secret.Data["ca.crt"]

	return &rest.Config{
		Host:            "https://" + net.JoinHostPort(host, port),
		TLSClientConfig: tlsClientConfig,
		BearerToken:     string(secret.Data["token"]),
	}, nil
}

func lookupServiceAccount(ctx context.Context, client kubernetes.Interface, workspace, namespace string) (*v1.ServiceAccount, error) {
	cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, workspace, metav1.GetOptions{})

	if err != nil {
		return nil, err
	}

	serviceAccountName, ok := cm.Data[cmKeyServiceAccount]

	if !ok {
		serviceAccountName = DefaultServiceAccountName
	}

	return client.CoreV1().ServiceAccounts(workspace).Get(ctx, serviceAccountName, metav1.GetOptions{})
}
