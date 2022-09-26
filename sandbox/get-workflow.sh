#!/bin/sh
wft=$1
ns=${2:-sandbox-dev}
shift
shift

if [[ -z $SANDBOX_TOKEN ]]
then
    echo "Export env SANDBOX_TOKEN to an appropriate jwt token"
    exit 1
fi

path="http://localhost:8842/api/v1/workflows/$ns/$wft"

curl -X 'GET' $path \
-H 'accept: application/json' \
-H "authorization: bearer $SANDBOX_TOKEN" \
$@