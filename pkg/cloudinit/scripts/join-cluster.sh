#!/bin/bash -xe

## Assumptions:
## - k8s is installed
## - /capi/etc/microcluster-address contains the address to use for microcluster
## - /capi/etc/join-token is a valid join token

address="$(cat /capi/etc/microcluster-address)"
name="$(cat /capi/etc/node-name)"
config_file="/capi/etc/config.yaml"
token="$(cat /capi/etc/join-token)"

max_attempts=3
delay=60
current_attempt=1

while [ $current_attempt -le $max_attempts ]; do

  echo "Attempt $current_attempt of $max_attempts to join cluster..."
  k8s join-cluster "${token}" --name "${name}" --address "${address}" --file "${config_file}"
  clusterJoinExitCode=$?
  if [ $clusterJoinExitCode -eq 0 ]; then
    echo "Cluster join succeeded"
    exit 0
  else
    echo "Cluster join failed"
    if [ $current_attempt -lt $max_attempts ]; then
      echo "Retrying in $delay seconds..."
      sleep $delay
    fi
  fi
  current_attempt=$((current_attempt + 1))
done

echo "Failed to join cluster after $max_attempts attempts"
exit 1
