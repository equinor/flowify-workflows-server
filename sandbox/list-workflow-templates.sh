#!/bin/sh
ns=${1:-sandbox-dev}
shift

echo "Namespace: $ns" >&2

if [[ -z $SANDBOX_TOKEN ]]
then
    echo "Export env SANDBOX_TOKEN to an appropriate jwt token"
    exit 1
fi
curl -X 'GET' "http://localhost:8842/api/v1/workflow-templates/$ns" \
-H 'accept: application/json' \
-H "authorization: bearer $SANDBOX_TOKEN" \
$@