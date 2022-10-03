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
  echo -e ${GREEN}
  echo =====================================================================
  echo Kind cluster exist, getting kubeconfig from cluster
  echo Modifying Kubernetes config to point to Kind master node
  echo =====================================================================
  echo -e ${NOCOLOR}
  sed -i "s/^    server:.*/    server: https:\/\/$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/" $HOME/.kube/config
else 
  echo -e ${RED}
  echo =====================================================================
  echo Kind cluster doesn\'t exist, server cannot be run
  echo =====================================================================
  echo -e ${NOCOLOR}
  exit -1
fi

echo -e ${BLUE}
echo =====================================================================
echo Deploying flowify server
echo =====================================================================
echo -e ${NOCOLOR}
bash -c 'FLOWIFY_K8S_NAMESPACE=argo $GOPATH/src/github.com/equinor/flowify-workflows-server/build/flowify-workflows-server --flowify-auth azure-oauth2-openid-token'
