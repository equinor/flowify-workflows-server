#!/bin/sh

# token not used
# token=$(yq .token secrets.yaml)

curl -X 'GET' "http://localhost:8842/api/v1/flowify-workflows/" \
-H 'accept: application/json' \
$@