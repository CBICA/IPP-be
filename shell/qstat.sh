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

for jid_eid in $(cat jids); do
    jid=$(echo $jid_eid | cut -d' ' -f1)
    eid=$(echo $jid_eid | cut -d' ' -f2)
    exists=$(qstat | grep $jid | wc -l)
    if [ $exists -eq 0 ]; then
        # remove jid from file
        sed -i "/${jid}/d" jids
        sed -i "/${jid} ${eid}/d" jid_eid
        # zip results dir
        zip -r $eid.zip $OUTPUT_DIR/$eid
        # push results
        success=$(curl -s -X POST -F "file=@${eid}.zip" $API_URL/$eid/results)
        if [ $success = "true" ]; then
            # remove zip, inputs, outputs
            rm $jid.zip
            rm -rf $OUTPUT_DIR/$jid
            rm -rf $INPUT_DIR/$jid
        else
            echo "Failed to push results for $eid"
            echo $success
        fi
    fi
done

kill -9 $pid
