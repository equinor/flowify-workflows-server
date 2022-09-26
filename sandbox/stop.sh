#!/usr/bin/env bash
#set +x
set -eu -o pipefail

killall flowify-workflows-server -v || echo 'flowify-workflows-server already stopped'
pushd $GOPATH > /dev/null
# assumes argo workflow-controller is installed in the same tree
controller=$(find . -wholename "*/dist/workflow-controller" | xargs realpath)
popd > /dev/null


# Launch the Argo controller
# Required env: client cert, client key, kub server host and port envs
killall ${controller##*/} -v || echo "${controller##*/} already stopped"
