#!/bin/bash

set -o errexit

function create_env_file() {
	local target_file="$1"
	shift 1
	local append_vars=("${@}")

	# Return if env file exists
	if [ -e "$target_file" ]; then
		return
	fi

	printf "# This is a configuration file for running tests.\n# Feel free to modify the values to suit your needs.\n\n" >"$target_file"

	for var in "${append_vars[@]}"; do
		echo "$var" >>"$target_file"
	done
}

test_dir="$1"
mkdir -p "$test_dir"

# Create the .env.test file
env_vars=(
    "PMM_VERSION=${TestVersion} #pmm-server/pmm-client image version"
    "PMM_SERVER_IMAGE=${PerconaServerUrl}"
    "PMM_CLIENT_IMAGE=${PerconaClientUrl}"
    "MONGO_IMAGE=mongo"
    "MONGO_TAG=latest"
	"USE_EXISTING_PMM=false # use existing pmm-server container"
	"PMM_URL=http://admin:admin@localhost # pmm-server url (used only while USE_EXISTING_PMM=true)"
)

env_file="$test_dir/.env.test"
create_env_file "$env_file" "${env_vars[@]}"

# Create the .env2.test file
second_env_file="$test_dir/.env2.test"
create_env_file "$second_env_file" "${env_vars[@]}"
