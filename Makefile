.PHONY: test vet compose-config up smoke down

COMPOSE_FILE ?= deploy/docker-compose.yaml

test:
	go test ./...

vet:
	go vet ./...

compose-config:
	docker compose -f $(COMPOSE_FILE) config

up:
	docker compose -f $(COMPOSE_FILE) up --build -d

smoke:
	./deploy/smoke.sh

down:
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans
