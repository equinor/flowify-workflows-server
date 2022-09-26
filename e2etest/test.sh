#!/usr/bin/env bash

set -eu -o pipefail

function cleanup {
    set +e  # Continue cleaning up if there is an issue

    printf 'Test shell script cleanup\n'

    kubectl delete ns test --ignore-not-found
    kubectl delete ns test-no-access --ignore-not-found

    # Quit the Argo conroller

    argopid=$(ps -ef | grep [w]orkflow-controller | tr -s ' '| cut -f 2 -d ' ')

    if [[ ! -z ${argopid} ]]; then
        kill $argopid
    fi

    # Quit the Flowify server
    flowifypid=$(ps -ef | grep [f]lowify-server | tr -s ' '| cut -f 2 -d ' ')

    if [[ ! -z ${flowifypid} ]]; then
        kill $flowifypid
    fi

}

trap cleanup EXIT

pushd ..

# Start a kubernetes cluster
minikube start
kubectl create namespace test --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace test-no-access --dry-run=client -o yaml | kubectl apply -f -

# Inject the default service account with the corresponding roles
kubectl apply -f e2etest/default-roles.yaml

# Copy artifact configmap to test namespace
kubectl get cm artifact-repositories --namespace=argo -o yaml |  grep -v '^\s*namespace:\s' | kubectl apply --namespace=test -f -
kubectl get secret my-minio-cred --namespace=argo -o yaml |  grep -v '^\s*namespace:\s' | kubectl apply --namespace=test -f -

# Launch the flowify server
export KUBERNETES_SERVICE_HOST=$(kubectl config view --minify | grep server | cut -f 3 -d "/" | cut -d ":" -f 1)
export KUBERNETES_SERVICE_PORT=$(kubectl config view --minify | grep server | cut -f 4 -d ":")

export FLOWIFY_MONGO_ADDRESS=localhost
export FLOWIFY_MONGO_PORT=27017

./build/flowify-workflows-server -v 7 > /dev/null 2>& 1 &

# Prints the PID of the flowify server so we can hook-up a debugger
ps -ef | grep [f]lowify-server | tr -s ' ' | cut -f 2 -d ' '

# Start a MongoDB server
docker container start $(docker container ls --all | grep mongo | cut -f 1 -d ' ') > /dev/null 2>& 1

cd $GOPATH
controller=$(find . -wholename "*/dist/workflow-controller")

# Launch the Argo controller
PNS_PRIVILEGED=true DEFAULT_REQUEUE_TIME=100ms LEADER_ELECTION_IDENTITY=local ALWAYS_OFFLOAD_NODE_STATUS=false OFFLOAD_NODE_STATUS_TTL=30s WORKFLOW_GC_PERIOD=30s UPPERIO_DB_DEBUG=0 ARCHIVED_WORKFLOW_GC_PERIOD=30s $controller --executor-image argoproj/argoexec:v3.1.13 --namespaced=true --namespace test > /dev/null 2>& 1 &

popd

unset KUBERNETES_SERVICE_HOST
unset KUBERNETES_SERVICE_PORT

# Run all e2e tests (the tests in this directory)
go test .


