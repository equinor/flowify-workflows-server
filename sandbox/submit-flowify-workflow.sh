#!/bin/sh

ns=$1
shift
wf=$1
shift

ver=${1}
shift

if [[ -z "$ver" ]];
then
data='{
  "resourceKind": "WorkflowTemplate",
  "ResourceName": '\"$wf\"'
}'
else
data='{
  "resourceKind": "WorkflowTemplate",
  "ResourceName": '\"$wf\"',
  "version": '\"$ver\"' 
}'
fi

path="http://localhost:8842/api/v1/workflows/$ns/submit"
echo $path >&2
echo $data >&2

curl -X 'POST' \
  "$path" \
  -H "authorization: bearer $SANDBOX_TOKEN" \
  -d "$data" \
$@