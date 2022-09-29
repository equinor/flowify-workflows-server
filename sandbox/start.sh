#!/usr/bin/env bash
#set +x
set -eu -o pipefail

# Start a kubernetes cluster
minikube start

# Inject the default service account with the corresponding roles
configns=sandbox-config
kubectl apply -f sandbox-config.yaml

# Copy artifact configmap to test namespace

# Launch the flowify server
export KUBERNETES_SERVICE_HOST=$(kubectl config view --minify | grep server | cut -f 3 -d "/" | cut -d ":" -f 1)
export KUBERNETES_SERVICE_PORT=$(kubectl config view --minify | grep server | cut -f 4 -d ":")

export FLOWIFY_MONGO_ADDRESS=localhost
export FLOWIFY_MONGO_PORT=27017

killall flowify-workflows-server -vq || printf "flowify-workflows-server not running, restarting\n"
../build/flowify-workflows-server -flowify-auth azure-oauth2-openid-token -namespace $configns & #> /tmp/test.out 2>& 1 &
printf "flowify-workflows-server started: $!\n" # Prints the PID of the flowify server so we can hook-up a debugger

# Start a MongoDB server
docker container start $(docker container ls --all | grep mongo | cut -f 1 -d ' ') > /dev/null 2>& 1

pushd $GOPATH > /dev/null
# assumes argo workflow-controller is installed in the same tree
controller=$(find . -wholename "*/dist/workflow-controller" | xargs realpath)
printf "Controller: $controller \n"
ARGO_CONTROLLER_VERSION=$($controller version 2> /dev/null)
printf "Version info: \n"
printf "$ARGO_CONTROLLER_VERSION\n"

popd > /dev/null

red=$(tput setaf 1)
green=$(tput setaf 2)
blue=$(tput setaf 33)
normal=$(tput sgr0)

# Launch the Argo controller, and check that versions match the executor
killall ${controller##*/}  -vq || printf "${controller##*/} not running, restarting\n"
ARGO_EXECUTOR_VERSION=v3.2.3
if [[ "$ARGO_CONTROLLER_VERSION" == *"$ARGO_EXECUTOR_VERSION"* ]]; then
  printf "${green}Argo controller/executor version check passed.${normal}\n"
else
  printf "${red}Argo controller/executor version check failed:${normal}\n"
  printf "$ARGO_CONTROLLER_VERSION ${red}does not match '${normal}$ARGO_EXECUTOR_VERSION'.\n"
  printf "${green}Either checkout the local controller at version matching ${normal}'${ARGO_EXECUTOR_VERSION}'${green},\n"
  printf "or update the variable ${normal}'ARGO_EXECUTOR_VERSION'${green} in this script.${normal}\n\n"
  exit 1 
fi

PNS_PRIVILEGED=true DEFAULT_REQUEUE_TIME=100ms LEADER_ELECTION_IDENTITY=local ALWAYS_OFFLOAD_NODE_STATUS=false OFFLOAD_NODE_STATUS_TTL=30s WORKFLOW_GC_PERIOD=30s UPPERIO_DB_DEBUG=0 ARCHIVED_WORKFLOW_GC_PERIOD=30s $controller --executor-image argoproj/argoexec:$ARGO_EXECUTOR_VERSION --namespaced=false > /dev/null 2>& 1 &
printf "workflow-controller started: $!\n"

