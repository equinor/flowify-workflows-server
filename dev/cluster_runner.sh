#!/usr/bin/env bash

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[0;37m'
NOCOLOR='\033[0m' # No Color

bash -c 'kind export --name cluster kubeconfig 2>/dev/null'
cluster_exist=$?

if [ $cluster_exist -eq 0 ]
then 
  echo Kind cluster exist, getting kubeconfig from cluster
else 
  echo Bringing up a cluster
  bash -c '/usr/local/bin/kind create cluster --name cluster'
fi

echo Modifying Kubernetes config to point to Kind master node
sed -i "s/^    server:.*/    server: https:\/\/$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/" $HOME/.kube/config
cd

echo -e ${BLUE}
echo =====================================================================
echo Applying argo config
echo =====================================================================
echo -e ${NOCOLOR}
kubectl apply -f $GOPATH/src/github.com/equinor/flowify-workflows-server/dev-config.yaml

if [ $cluster_exist -ne 0 ]
then
  echo -e ${BLUE}
  echo =====================================================================
  echo Deploying argo
  echo =====================================================================
  echo -e ${NOCOLOR}
  kubectl create ns $FLOWIFY_K8S_NAMESPACE
  kubectl apply -n $FLOWIFY_K8S_NAMESPACE -f https://raw.githubusercontent.com/argoproj/argo-workflows/master/manifests/quick-start-postgres.yaml
fi

echo -e ${BLUE}
echo =====================================================================
echo Applying argo config
echo =====================================================================
echo -e ${NOCOLOR}
kubectl apply -f $GOPATH/src/github.com/equinor/flowify-workflows-server/dev-config.yaml

# echo Setting up Kubectl Proxy
# CLIENT_IP=$(docker inspect --format='{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' container-name)
# kubectl proxy --address=$CLIENT_IP --accept-hosts=^localhost$,^127\.0\.0\.1$,^\[::1\]$ &

bash -c '$GOPATH/src/github.com/equinor/flowify-workflows-server/build/flowify-workflows-server --flowify-auth disabled-auth'
