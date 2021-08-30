#!/bin/bash

set -euf -o pipefail

if [ $# -ne 3 ]; then
    echo "Usage: $0 <local_port> <remote_port> <ssh_address>"
    exit 1
fi

cd $(dirname $0) # we start in the directory of the script
root_dir=$PWD
INPUT_DIR=$root_dir/inputs   # where to place downloaded files
OUTPUT_DIR=$root_dir/outputs # experdir
# setup ssh tunnel
LOCAL_PORT=$1
REMOTE_PORT=$2
SSH_ADDR=$3
API_URL="http://localhost:${LOCAL_PORT}/experiments"
ssh -qnNT -L ${LOCAL_PORT}:127.0.0.1:${REMOTE_PORT} ${SSH_ADDR} &
pid=$!
echo "Started SSH tunnel (:${REMOTE_PORT} => :${LOCAL_PORT}) with PID ${pid}"

# fetch experiments from queue
queue=$(curl -s -X GET ${API_URL}/queue | jq -c '.[]')
for experiment in $queue; do

    eid=$(echo $experiment | jq '.id')
    uid=$(echo $experiment | jq '.user')
    host=$(echo $experiment | jq '.host')
    app=$(echo $experiment | jq -r '.app')

    # download & unzip experiment files
    echo "Downloading experiment ${eid}"
    curl -sO -X GET "${API_URL}/${eid}/files"
    unzip -o files -d $INPUT_DIR
    rm files

    # format app's command line options according to experiment params
    app=$(cat "../IPP-Experiment_Defintions/${app}.json")
    command=$(echo $app | jq -r '.Executable')
    keys=$(echo $experiment | jq -r '.params | keys[]')
    experdir=$OUTPUT_DIR/$eid
    mkdir -p $experdir || true
    unused_defaults=$(echo $app | jq '.DEFAULTS')
    for key in $keys; do
        value=$(echo $experiment | jq -r ".params.${key}")
        unused_defaults=$(echo $unused_defaults | jq "del(.\"${key}\")")
        # check if value is empty string
        if [ -z "${value}" ]; then
            value=$(echo $app | jq ".DEFAULTS.${key}")
        fi
        # check if value is special experiment dir
        if [[ "${value}" == "\$experdir"* ]]; then
            value=$(echo $value | sed -e 's/\$experdir/'$experdir'/')
        fi
        # check if value references input file
        if [[ "${value}" == $(basename $INPUT_DIR)* ]]; then
            v=$(echo $value | sed -e 's/'$(basename $INPUT_DIR)'//')
            value="${INPUT_DIR}/${eid}${v}"
        fi

        # check if key is in params
        if [ $(echo $app | jq ".PARAMS | has(\"${key}\")") = "true" ]; then
            command="$command $(printf '%s %s' $(echo $app | jq -r '.PARAMS.'${key}) $value)"
        else # it must be in binopts
            command="$command $(printf $(echo $app | jq -r '.BINOPTS.'${key}))"
        fi
    done
    keys=$(echo $unused_defaults | jq -r 'keys[]')
    for key in $keys; do
        value=$(echo $unused_defaults | jq -r ".${key}")
        # check if value is special experiment dir
        if [[ "${value}" == "\$experdir"* ]]; then
            value=$(echo $value | sed -e 's/\$experdir/'$experdir'/')
        fi
        command="$command $(printf '%s %s' $key $value)"
    done
    cd $experdir
    resources=$(echo $app | jq -r '.SGEJobResources')
    set +x
    jid=$(qsub -cwd -b y -terse -o output.txt $resources $command)
    set -x
    cd $root_dir
    echo "$jid $eid" >>jids

done

kill -9 $pid
