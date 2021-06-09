.PHONY= build up down re pmm-status mongo-insert vm-export

PMMT_BIN_NAME?=pmm-transferer
PMM_DUMP_PATTERN?=pmm-dump-*.tar.gz

PMM_VM_URL?="http://admin:admin@localhost:8282/prometheus"

PMM_MONGO_USERNAME?=pmm_mongodb
PMM_MONGO_PASSWORD?=password
PMM_MONGO_URL?=mongodb:27017

ADMIN_MONGO_USERNAME?=admin
ADMIN_MONGO_PASSWORD?=admin

all: build up mongo-reg mongo-insert

build:
	go build -o $(PMMT_BIN_NAME) pmm-transferer/cmd/transferer

up:
	mkdir -p setup/pmm && touch setup/pmm/agent.yaml && chmod 0666 setup/pmm/agent.yaml
	docker-compose up -d
	sleep 5 # waiting for pmm server to be ready :(
	docker exec pmm-client pmm-agent setup

down:
	docker-compose down --volumes
	rm -rf setup/pmm

re: down up

pmm-status:
	docker exec pmm-client pmm-admin status

mongo-reg:
	docker exec pmm-client pmm-admin add mongodb \
		--username=$(PMM_MONGO_USERNAME) --password=$(PMM_MONGO_PASSWORD) mongo $(PMM_MONGO_URL)

mongo-insert:
	docker exec mongodb mongo -u $(ADMIN_MONGO_USERNAME) -p $(ADMIN_MONGO_PASSWORD) \
		--eval 'db.getSiblingDB("mydb").mycollection.insert( [{ "a": 1 }, { "b": 2 }] )' admin

vm-export:
	./$(PMMT_BIN_NAME) export --victoria_metrics_url=$(PMM_VM_URL)

clean:
	rm -f $(PMMT_BIN_NAME) $(PMM_DUMP_PATTERN)
