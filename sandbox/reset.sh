#!/usr/bin/env bash
#set +x


# Start a kubernetes cluster
minikube start

# Inject the default service account with the corresponding roles
kubectl delete -f sandbox-config.yaml

