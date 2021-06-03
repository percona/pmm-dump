.PHONY= build pmm-up pmm-down vm-export

PMMT_BIN_NAME?=pmm-transferer
PMM_VM_URL?="http://admin:admin@localhost:8282/prometheus"

all: build pmm-up

build:
	go build -o $(PMMT_BIN_NAME) pmm-transferer/cmd/transferer

pmm-up:
	docker-compose up -d

pmm-down:
	docker-compose down --volumes

vm-export:
	./$(PMMT_BIN_NAME) export --victoria_metrics_url=$(PMM_VM_URL)
