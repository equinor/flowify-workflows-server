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
  echo =====================================================================
  echo -e ${NOCOLOR}
else 
  echo -e ${BLUE}
  echo =====================================================================
  echo Bringing up a cluster
  echo =====================================================================
  echo -e ${NOCOLOR}
  bash -c '/usr/local/bin/kind create cluster --name cluster --config /root/kind.yaml'
fi

# Set a trap for SIGTERM signal
if ! [[ "$KEEP_KIND_CLUSTER_ALIVE" = true ]]
then
  trap "docker rm -f cluster-control-plane" SIGTERM
fi

echo -e ${GREEN}
echo =====================================================================
echo Modifying Kubernetes config to point to Kind master node
echo =====================================================================
echo -e ${NOCOLOR}
sed -i "s/^    server:.*/    server: https:\/\/$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT/" $HOME/.kube/config

if [ $cluster_exist -ne 0 ]
then
  echo -e ${BLUE}
  echo =====================================================================
  echo Deploying argo
  echo =====================================================================
  echo -e ${NOCOLOR}
  kubectl apply -k /root/argo-cluster-install

  echo -e ${PURPLE}
  echo =====================================================================
  echo "Waiting for deployment..."
  echo =====================================================================
  echo -e ${NOCOLOR}
  kubectl rollout status deployments -n argo
fi

sleep infinity
