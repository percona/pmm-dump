.PHONY= build pmm-up pmm-down vm-export

PMMT_BIN_NAME?=pmm-transferer
PMM_VM_URL?="http://admin:admin@localhost:8282/prometheus"
PMM_MONGO_USERNAME?=pmm_mongodb
PMM_MONGO_PASSWORD?=password
PMM_MONGO_URL?=mongodb:27017

all: build pmm-up

build:
	go build -o $(PMMT_BIN_NAME) pmm-transferer/cmd/transferer

pmm-up:
	mkdir -p setup/pmm && touch setup/pmm/agent.yaml && chmod 0666 setup/pmm/agent.yaml
	docker-compose up -d
	sleep 3 # waiting for pmm server to be ready :(
	docker exec pmm-client pmm-agent setup
	docker exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

pmm-down:
	docker-compose down --volumes
	rm -rf setup/pmm

pmm-status:
	docker exec pmm-client pmm-admin status

vm-export:
	./$(PMMT_BIN_NAME) export --victoria_metrics_url=$(PMM_VM_URL)
