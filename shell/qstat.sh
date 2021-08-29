#!/bin/bash

set -xeuf -o pipefail

cd $(dirname $0) # we start in the directory of the script
root_dir=$PWD
INPUT_DIR=$root_dir/inputs # where to place downloaded files
OUTPUT_DIR=$root_dir/outputs # experdir
# setup ssh tunnel
LOCAL_PORT=35095
REMOTE_PORT=5000
SSH_ADDR=root@localhost
API_URL="http://localhost:${LOCAL_PORT}/experiments"
ssh -qnNT -L ${LOCAL_PORT}:127.0.0.1:${REMOTE_PORT} ${SSH_ADDR} &
pid=$!
echo "Started SSH tunnel (:${REMOTE_PORT} => :${LOCAL_PORT}) with PID ${pid}"

for jid in $(cat jids); do
    exists=$(qstat | grep $jid | wc -l)
    if [ $exists -eq 0 ]; then
        # get experiment id
        eid=$(grep -E "^${jid} [0-9]+\$" jid_eid | cut -d' ' -f2)
        # remove jid from file
        sed -i "/${jid}/d" jids
        sed -i "/${jid} ${eid}/d" jid_eid
        # zip results dir
        zip -r $eid.zip $OUTPUT_DIR/$eid
        # push results
        curl -X POST -F "file=@${eid}.zip" $API_URL/$eid/results
        # remove zip, inputs, outputs
        rm $jid.zip
        rm -rf $OUTPUT_DIR/$jid
        rm -rf $INPUT_DIR/$jid
    fi
done

kill -9 $pid
