.PHONY: test vet compose-config compose-config-docker-runner runner-images up smoke smoke-real-docker smoke-real-gvisor smoke-runner-capacity down

COMPOSE_FILE ?= deploy/docker-compose.yaml
DOCKER_RUNNER_COMPOSE_FILES ?= deploy/docker-compose.yaml:deploy/docker-compose.docker-runner.yaml
RUNNER_IMAGE_GO ?= soj-runner-go:local
RUNNER_IMAGE_CPP17 ?= soj-runner-cpp17:local
SOJ_DOCKER_RUNNER_WORKDIR ?= /tmp/soj-runner-work

test:
	go test ./...

vet:
	go vet ./...

compose-config:
	docker compose -f $(COMPOSE_FILE) config

compose-config-docker-runner:
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) docker compose config

runner-images:
	docker build -f deploy/runner-images/go/Dockerfile -t $(RUNNER_IMAGE_GO) deploy/runner-images/go
	docker build -f deploy/runner-images/cpp17/Dockerfile -t $(RUNNER_IMAGE_CPP17) deploy/runner-images/cpp17

up:
	docker compose -f $(COMPOSE_FILE) up --build -d

smoke:
	./deploy/smoke.sh

smoke-real-docker: runner-images
	mkdir -p $(SOJ_DOCKER_RUNNER_WORKDIR)
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	docker compose down -v --remove-orphans
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	docker compose up --build -d
	COMPOSE_FILES=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SMOKE_REAL_JUDGE=1 \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	./deploy/smoke.sh

smoke-real-gvisor: runner-images
	mkdir -p $(SOJ_DOCKER_RUNNER_WORKDIR)
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	./scripts/dev/check-docker-runner.sh
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	docker compose down -v --remove-orphans
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	docker compose up --build -d
	COMPOSE_FILES=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SMOKE_REAL_JUDGE=1 \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	./deploy/smoke.sh

smoke-runner-capacity: runner-images
	mkdir -p $(SOJ_DOCKER_RUNNER_WORKDIR)
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=$(SOJ_DOCKER_RUNNER_RUNTIME) \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	docker compose down -v --remove-orphans
	COMPOSE_FILES=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=$(SOJ_DOCKER_RUNNER_RUNTIME) \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_DOCKER_RUNNER_IMAGE_GO=$(RUNNER_IMAGE_GO) \
	SOJ_DOCKER_RUNNER_IMAGE_CPP17=$(RUNNER_IMAGE_CPP17) \
	./scripts/dev/runner-capacity-smoke.sh

down:
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans
