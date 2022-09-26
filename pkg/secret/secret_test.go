package secret

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	workspaceName  = "dummy-workspace"
	workspaceName2 = "dummy-workspace2"
)

func Test_SecretClientRoundTrip(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	client := NewSecretClient(clientSet)
	ctx := context.TODO()

	err := client.AddSecretKey(ctx, workspaceName, "key1", "value1")
	require.NoError(t, err)

	err = client.AddSecretKey(ctx, workspaceName, "key2", "value2")
	require.NoError(t, err)

	err = client.AddSecretKey(ctx, workspaceName, "key3", "value3")
	require.NoError(t, err)

	err = client.AddSecretKey(ctx, workspaceName2, "key3", "value1")
	require.NoError(t, err)

	err = client.AddSecretKey(ctx, workspaceName2, "key4", "value4")
	require.NoError(t, err)

	keys, err := client.ListAvailableKeys(ctx, workspaceName)
	require.NoError(t, err)

	require.Len(t, keys, 3)
	require.ElementsMatch(t, keys, []string{"key1", "key2", "key3"})

	lst1, err := clientSet.CoreV1().Secrets(workspaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, lst1.Items, 1)

	lst2, err := clientSet.RbacV1().Roles(workspaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, lst2.Items, 1)

	lst3, err := clientSet.RbacV1().RoleBindings(workspaceName).List(ctx, metav1.ListOptions{})
	require.NoError(t, err)
	require.Len(t, lst3.Items, 1)
}

func Test_SecretDelete(t *testing.T) {
	clientSet := fake.NewSimpleClientset()
	client := NewSecretClient(clientSet)
	ctx := context.TODO()

	// Fill db with dummy values
	for _, key := range []string{"key1", "key2"} {
		require.NoError(t, client.AddSecretKey(ctx, workspaceName, key, "value1"))
	}

	require.NoError(t, client.DeleteSecretKey(ctx, workspaceName, "key1"))

	secret, err := clientSet.CoreV1().Secrets(workspaceName).Get(ctx, DefaultObjectName, metav1.GetOptions{})
	require.NoError(t, err)

	require.Len(t, secret.Data, 1, "key1 should be removed")
	require.Equal(t, "value1", string(secret.Data["key2"]), "key2 should be unaffected")

	require.Error(t, client.DeleteSecretKey(ctx, workspaceName, "key1"), "Delete previously existing key")
	require.NoError(t, client.DeleteSecretKey(ctx, workspaceName, "key2"), "Delete remaining key")

	secret, err = clientSet.CoreV1().Secrets(workspaceName).Get(ctx, DefaultObjectName, metav1.GetOptions{})
	require.NoError(t, err)

	require.Len(t, secret.Data, 0, "Both keys deleted, secret should be empty")
}
