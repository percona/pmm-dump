#!/bin/bash

set -o errexit

function create_env_file() {
	local source_file="$1"
	local target_file="$2"
	shift 2
	local append_vars=("${@}")

	# Return if env file exists
	if [ -e "$target_file" ]; then
		return
	fi

	cp "$source_file" "$target_file"
	sed -i '1i# This is a configuration file for running tests.\n# Feel free to modify the values to suit your needs.\n' "$target_file"
	sed -i '/PMM_SERVER_NAME\|PMM_MONGO_NAME\|PMM_CLIENT_NAME\|PMM_AGENT_CONFIG_FILE\|PMM_HTTP_PORT\|PMM_HTTPS_PORT\|CLICKHOUSE_PORT\|CLICKHOUSE_PORT_HTTP\|MONGO_PORT/d' "$target_file"

	for var in "${append_vars[@]}"; do
		echo "$var" >>"$target_file"
	done
}

base_file=".env.example"

test_dir="$1"
mkdir -p "$test_dir"

# Create the .env.test file
env_vars=(
	"PMM_SERVER_NAME=pmm-server-test # pmm-server container name"
	"PMM_CLIENT_NAME=pmm-client-test # pmm-client container name"
	"PMM_MONGO_NAME=mongo-test # mongo container name"
	""
	"PMM_HTTP_PORT=8282"
	"PMM_HTTPS_PORT=8384"
	"CLICKHOUSE_PORT=9001"
	"CLICKHOUSE_PORT_HTTP=8124"
	"MONGO_PORT=27018"
	""
	"USE_EXISTING_PMM=false # use existing pmm-server container"
	"PMM_URL=http://admin:admin@localhost # pmm-server url (used only while USE_EXISTING_PMM=true)"
)

env_file="$test_dir/.env.test"
create_env_file "$base_file" "$env_file" "${env_vars[@]}"

# Create the .env2.test file
env_vars=(
	"PMM_SERVER_NAME=pmm-server-test-2 # pmm-server container name"
	"PMM_CLIENT_NAME=pmm-client-test-2 # pmm-client container name"
	"PMM_MONGO_NAME=mongo-test-2 # mongo container name"
	""
	"PMM_HTTP_PORT=8283"
	"PMM_HTTPS_PORT=8385"
	"CLICKHOUSE_PORT=9002"
	"CLICKHOUSE_PORT_HTTP=8125"
	"MONGO_PORT=27019"
	""
	"USE_EXISTING_PMM=false # use existing pmm-server container"
	"PMM_URL=http://admin:admin@localhost # pmm-server url (used only while USE_EXISTING_PMM=true)"
)
second_env_file="$test_dir/.env2.test"
create_env_file "$base_file" "$second_env_file" "${env_vars[@]}"
