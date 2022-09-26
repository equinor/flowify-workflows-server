#!/bin/sh
#set -x

ns=${1:-sandbox-project-a}
shift

fn=${1:--}
shift

if [[ -z $SANDBOX_TOKEN ]]
then
    echo "Export env SANDBOX_TOKEN to an appropriate jwt token"
    exit 1
fi

curl -X 'POST' \
  "http://localhost:8842/api/v1/workflow-templates/$ns/lint" \
  -H 'accept: application/json' \
  -H 'Content-Type: application/json' \
  -H "authorization: bearer $SANDBOX_TOKEN" \
   -d @$fn \
$@