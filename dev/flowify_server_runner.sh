#!/usr/bin/env bash

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
WHITE='\033[0;37m'
NOCOLOR='\033[0m' # No Color

bash kind_cluster_config_export.sh
cluster_exists=$?

if [ "$cluster_exist" -neq 0 ]
then
echo -e ${RED}
echo =====================================================================
echo Cluster does not exist, cannot continue
echo =====================================================================
echo -e ${NOCOLOR}
exit $cluster_exists
fi

echo -e ${BLUE}
echo =====================================================================
echo Deploying flowify server
echo =====================================================================
echo -e ${NOCOLOR}

bash -c '$GOPATH/src/github.com/equinor/flowify-workflows-server/build/flowify-workflows-server'
