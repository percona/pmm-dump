.PHONY= build pmm-up pmm-down vm-export

PMMT_BIN_NAME?=pmm-transferer
PMM_VM_URL?="http://admin:admin@localhost:8282/prometheus"

all: build pmm-up

build:
	go build -o $(PMMT_BIN_NAME) pmm-transferer/cmd/transferer

pmm-up:
	mkdir -p pmm && touch pmm/agent.yaml && chmod 0666 pmm/agent.yaml
	docker-compose up -d
	sleep 3 # waiting for pmm server to be ready :(
	docker exec pmm-client pmm-agent setup

pmm-down:
	docker-compose down --volumes
	rm -rf pmm

vm-export:
	./$(PMMT_BIN_NAME) export --victoria_metrics_url=$(PMM_VM_URL)
