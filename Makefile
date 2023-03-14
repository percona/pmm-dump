.PHONY= build up down re pmm-status mongo-reg mongo-insert export-all import-all clean up-test down-test run-tests

PMMD_BIN_NAME?=pmm-dump
PMM_DUMP_PATTERN?=pmm-dump-*.tar.gz

PMM_URL?="http://admin:admin@localhost:8282"
PMM_VM_URL?="http://admin:admin@localhost:8282/prometheus"
PMM_CH_URL?="http://localhost:9000?database=pmm"

PMM_MONGO_USERNAME?=pmm_mongodb
PMM_MONGO_PASSWORD?=password
PMM_MONGO_URL?=mongodb:27017

ADMIN_MONGO_USERNAME?=admin
ADMIN_MONGO_PASSWORD?=admin

DUMP_FILENAME=dump.tar.gz

BRANCH:=$(shell git branch --show-current)
COMMIT:=$(shell git rev-parse --short HEAD)
VERSION:=$(shell git describe --tags --abbrev=0)

all: build re mongo-reg mongo-insert export-all re import-all

build:
	go build -ldflags "-X 'main.GitBranch=$(BRANCH)' -X 'main.GitCommit=$(COMMIT)' -X 'main.GitVersion=$(VERSION)'" -o $(PMMD_BIN_NAME) pmm-dump/cmd/pmm-dump

up:
	mkdir -p setup/pmm && touch setup/pmm/agent.yaml && chmod 0666 setup/pmm/agent.yaml
	docker compose up -d
	sleep 15 # waiting for pmm server to be ready :(
	docker compose exec pmm-client pmm-agent setup || true

down:
	docker compose down --volumes
	rm -rf setup/pmm/agent.yaml

up-test:
	mkdir -p setup/pmm && touch setup/pmm/agent_test.yaml && chmod 0666 setup/pmm/agent_test.yaml
	docker compose -p test --env-file .env.test up -d
	sleep 15 # waiting for pmm server to be ready :(
	docker compose -p test exec pmm-client pmm-agent setup || true

down-test:
	docker compose -p test --env-file .env.test down --volumes
	rm -rf setup/pmm/agent_test.yaml

re: down up

pmm-status:
	docker compose exec pmm-client pmm-admin status

mongo-reg:
	docker compose exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

mongo-reg-test:
	docker compose -p test exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

mongo-insert:
	docker compose exec mongodb mongosh -u $(ADMIN_MONGO_USERNAME) -p $(ADMIN_MONGO_PASSWORD) \
		--eval 'db.getSiblingDB("mydb").mycollection.insertMany( [{ "a": 1 }, { "b": 2 }] )' admin

mongo-insert-test:
	docker compose -p test exec mongodb mongosh -u $(ADMIN_MONGO_USERNAME) -p $(ADMIN_MONGO_PASSWORD) \
		--eval 'db.getSiblingDB("mydb").mycollection.insertMany( [{ "a": 1 }, { "b": 2 }] )' admin

export-all:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan

export-vm:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core

export-ch:
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-qan

import-all:
	./$(PMMD_BIN_NAME) import -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan

run-tests: build down-test
	go test -v -p 1 -timeout 2000s ./...

clean:
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) $(DUMP_FILENAME)
