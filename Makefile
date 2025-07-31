.PHONY: all build up down re pmm-status mongo-reg mongo-insert export-all export-vm export-ch import-all init-test run-tests clean init

export CGO_ENABLED=0

PMMD_BIN_NAME?=pmm-dump
PMM_DUMP_PATTERN?=pmm-dump-*.tar.gz

PMM_HTTP_PORT?=8281

PMM_URL?="http://admin:admin@localhost:$(PMM_HTTP_PORT)"

PMM_MONGO_USERNAME?=pmm_mongodb
PMM_MONGO_PASSWORD?=password
PMM_MONGO_URL?=mongodb:27017
CLICKHOUSE_URL=clickhouse://default:password@0.0.0.0:9000/pmm

TEST_CFG_DIR=test

ADMIN_MONGO_USERNAME?=admin
ADMIN_MONGO_PASSWORD?=admin
DUMP_FILENAME=dump.tar.gz
ENCRYPTED_DUMP_FILENAME=dump.tar.gz.enc

BRANCH:=$(shell git branch --show-current)
COMMIT:=$(shell git rev-parse --short HEAD)
VERSION:=$(shell git describe --tags --abbrev=0)

all: build re mongo-reg mongo-insert export-all re import-all

init:                   ## Install development tools
	cd tools && go generate -x -tags=tools
	bash -c "[ ! -f .env ] && cp .env.example .env || true"

build:
	go build -ldflags "-X 'main.GitBranch=$(BRANCH)' -X 'main.GitCommit=$(COMMIT)' -X 'main.GitVersion=$(VERSION)'" -o $(PMMD_BIN_NAME) pmm-dump/cmd/pmm-dump

format:                 ## Format source code
	bin/gofumpt -l -w .
	bin/goimports -local github.com/percona/pmm-dump -l -w .

check:                  ## Run checks/linters for the whole project
	bin/license-eye -c .licenserc.yaml header check
	bin/go-consistent -pedantic ./...
	LOG_LEVEL=error bin/golangci-lint run

up: init
	mkdir -p setup/pmm && touch setup/pmm/agent.yaml && chmod 0666 setup/pmm/agent.yaml
	docker compose up -d
	sleep 15 # waiting for pmm server to be ready :(
	docker compose exec pmm-client pmm-agent setup
	docker compose exec pmm-server sed -i 's#<password_sha256_hex>[^<]*</password_sha256_hex>#<password_sha256_hex>5e884898da28047151d0e56f8dc6292773603d0d6aabbdd62a11ef721d1542d8</password_sha256_hex>#g' /etc/clickhouse-server/users.xml
	docker compose exec pmm-server sed -i 's#<allow_plaintext_password>0</allow_plaintext_password>#<allow_plaintext_password>1</allow_plaintext_password>#g' /etc/clickhouse-server/config.xml
	docker compose exec pmm-server sed -i 's#<!-- <listen_host>0.0.0.0</listen_host> -->#<listen_host>0.0.0.0</listen_host>#g' /etc/clickhouse-server/config.xml
	docker compose exec pmm-server supervisorctl restart clickhouse

down:
	docker compose down --volumes
	rm -rf setup/pmm/agent.yaml

down-tests:
	./support-files/destroy-test-resources

re: down up

pmm-status:
	docker compose exec pmm-client pmm-admin status

mongo-reg:
	docker compose exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

mongo-insert:
	docker compose exec mongodb mongosh -u $(ADMIN_MONGO_USERNAME) -p $(ADMIN_MONGO_PASSWORD) \
		--eval 'db.getSiblingDB("mydb").mycollection.insertMany( [{ "a": 1 }, { "b": 2 }] )' admin

export-all-random: ## export with random generated password
	./$(PMMD_BIN_NAME) export --allow-insecure-certs -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --click-house-url=$(CLICKHOUSE_URL)

export-all-random-to-file: ## export with random generated password to file
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --pass-filepath pass.txt --click-house-url=$(CLICKHOUSE_URL)

export-all-random-just-key: ## export with random generated password without logs
	./$(PMMD_BIN_NAME) export --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --just-key --click-house-url=$(CLICKHOUSE_URL)

export-all: ## export with provided password
	./$(PMMD_BIN_NAME) export --allow-insecure-certs -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --pass somepass --click-house-url=$(CLICKHOUSE_URL)

export-all-to-file: ## export with provided password to file
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --pass somepass --pass-filepath pass.txt --click-house-url=$(CLICKHOUSE_URL)

export-all-just-key: ## export with providedd password without logs
	./$(PMMD_BIN_NAME) export --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --pass somepass --just-key --click-house-url=$(CLICKHOUSE_URL)

export-all-no-encryption: ## export without encryption
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --no-encryption --click-house-url=$(CLICKHOUSE_URL)

export-vm: ## export vm with random generated password
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --click-house-url=$(CLICKHOUSE_URL)

export-ch: ## export ch with random generated password
	./$(PMMD_BIN_NAME) export -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-qan --no-dump-core --click-house-url=$(CLICKHOUSE_URL)

import-all: ## import with provided password
	./$(PMMD_BIN_NAME) import -v --dump-path $(ENCRYPTED_DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --pass somepass --click-house-url=$(CLICKHOUSE_URL)

import-all-no-encryption: ## import without encryption
	./$(PMMD_BIN_NAME) import -v --dump-path $(DUMP_FILENAME) \
		--pmm-url=$(PMM_URL) --dump-core --dump-qan --no-encryption --click-house-url=$(CLICKHOUSE_URL)
		
clean:
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) $(DUMP_FILENAME)
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) "$(DUMP_FILENAME)"
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) "$(ENCRYPTED_DUMP_FILENAME)"
	rm -f $(PMMD_BIN_NAME) $(PMM_DUMP_PATTERN) $(ENCRYPTED_DUMP_FILENAME)
	rm -rf $(TEST_CFG_DIR)/pmm $(TEST_CFG_DIR)/tmp

run-e2e-tests: export PMM_DUMP_MAX_PARALLEL_TESTS=3

run-e2e-tests-v2: export PMM_DUMP_MAX_PARALLEL_TESTS=3

run-e2e-tests-v2: init-e2e-tests
	./support-files/run-tests v2

run-e2e-tests: init-e2e-tests
	./support-files/run-tests e2e

run-validate-tests: init-e2e-tests
	./support-files/run-tests validate

run-unit-tests:
	./support-files/run-tests

init-e2e-tests: init build
	./setup/test/init-test-configs.sh test

run-tests: run-unit-tests run-e2e-tests run-e2e-tests-v2

