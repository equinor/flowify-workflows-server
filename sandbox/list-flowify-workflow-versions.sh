#!/bin/sh

# token not used
# token=$(yq .token secrets.yaml)

fwf=$1
shift

curl -X 'GET' "http://localhost:8842/api/v1/flowify-workflows/${fwf}/versions/" \
-H 'accept: application/json' \
$@