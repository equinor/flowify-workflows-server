#!/bin/sh
set +x

wf=${1:-hello-world}
shift
ver=${1}
shift

path="http://localhost:8842/api/v1/flowify-workflows/$wf"

if [[ -n $ver ]];
then
    #append query
    path="$path?version=$ver"
fi

# token not used
# token=$(yq .token secrets.yaml)

#echo $path

curl -X 'GET' "$path" \
-H 'accept: application/json' \
$@