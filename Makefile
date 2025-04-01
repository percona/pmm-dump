.PHONY: all build up down re pmm-status mongo-reg mongo-insert export-all export-vm export-ch import-all init-test run-tests clean init

export CGO_ENABLED=0

PMMD_BIN_NAME?=pmm-dump
PMM_DUMP_PATTERN?=pmm-dump-*.tar.gz

PMM_HTTP_PORT?=8282

PMM_URL?="http://admin:admin@localhost:$(PMM_HTTP_PORT)"

PMM_MONGO_USERNAME?=pmm_mongodb
PMM_MONGO_PASSWORD?=password
PMM_MONGO_URL?=mongodb:27017

TEST_CFG_DIR=test

ADMIN_MONGO_USERNAME?=admin
ADMIN_MONGO_PASSWORD?=admin
DUMP_FILENAME=dump.tar.gz

BRANCH:=$(shell git branch --show-current)
COMMIT:=$(shell git rev-parse --short HEAD)
VERSION:=$(shell git describe --tags --abbrev=0)

all: build re mongo-reg mongo-insert export-all re import-all

init:
	bash -c "[ ! -f .env ] && cp .env.example .env || true"

build:
	go build -ldflags "-X 'main.GitBranch=$(BRANCH)' -X 'main.GitCommit=$(COMMIT)' -X 'main.GitVersion=$(VERSION)'" -o $(PMMD_BIN_NAME) pmm-dump/cmd/pmm-dump

up: init
	mkdir -p setup/pmm && touch setup/pmm/agent.yaml && chmod 0666 setup/pmm/agent.yaml
	docker compose up -d
	sleep 15 # waiting for pmm server to be ready :(
	docker compose exec pmm-client pmm-agent setup || true

down:
	docker compose down --volumes
	rm -rf setup/pmm/agent.yaml

down-tests:
	docker compose ls -q | grep '^pmm-dump-test-' | while read -r project; do COMPOSE_PROJECT_NAME="$$project" docker compose down --volumes; done

re: down up

pmm-status:
	docker compose exec pmm-client pmm-admin status

mongo-reg:
	docker compose exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

mongo-insert:
	docker compose exec mongodb mongosh -u $(ADMIN_MONGO_USERNAME) -p $(ADMIN_MONGO_PASSWORD) \
		--eval 'db.getSiblingDB("mydb").mycollection.insertMany( [{ "a": 1 }, { "b": 2 }] )' admin

export-all:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan

export-vm:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --chunk-time-range='60m'

export-ch:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-qan

import-all:
	./$(PMMD_BIN_NAME) import -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan

init-test: init build
	./setup/test/init-test-configs.sh test

run-tests: init-test down-tests build
	./support-files/run-tests	

clean:
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) $(DUMP_FILENAME)
	rm -rf $(TEST_CFG_DIR)/pmm $(TEST_CFG_DIR)/tmp
