#!/bin/sh

if [[ -z $SANDBOX_TOKEN ]]
then
    echo "Export env SANDBOX_TOKEN to an appropriate jwt token"
    exit 1
fi
curl -X 'GET' "http://localhost:8842/api/v1/userinfo" \
-H 'accept: application/json' \
-H "authorization: bearer $SANDBOX_TOKEN" \
$@
