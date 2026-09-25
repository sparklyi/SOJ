.PHONY: test vet lint compose-config compose-config-docker-runner config-print runner-images runner-images-pull runner-images-build up smoke smoke-real-docker smoke-real-gvisor smoke-runner-capacity down

COMPOSE_FILE ?= deploy/docker-compose.yaml
DOCKER_RUNNER_COMPOSE_FILES ?= deploy/docker-compose.yaml:deploy/docker-compose.docker-runner.yaml
RUNNER_IMAGE_REGISTRY ?= ghcr.io/sparklyi
RUNNER_IMAGE_TAG ?= main
RUNNER_IMAGE_GO ?= $(RUNNER_IMAGE_REGISTRY)/soj-runner-go:$(RUNNER_IMAGE_TAG)
RUNNER_IMAGE_CPP17 ?= $(RUNNER_IMAGE_REGISTRY)/soj-runner-cpp17:$(RUNNER_IMAGE_TAG)
LOCAL_RUNNER_IMAGE_GO ?= soj-runner-go:local
LOCAL_RUNNER_IMAGE_CPP17 ?= soj-runner-cpp17:local
RUNNER_IMAGES_PREPARE ?= pull
SOJ_DOCKER_RUNNER_WORKDIR ?= /tmp/soj-runner-work

test:
	go test ./...

vet:
	go vet ./...

# Requires golangci-lint v2 (https://golangci-lint.run/welcome/install/).
lint:
	golangci-lint run

compose-config:
	docker compose -f $(COMPOSE_FILE) config

compose-config-docker-runner:
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) docker compose config

config-print:
	go run ./cmd/soj-api --config deploy/config.yaml --print-config

runner-images:
ifeq ($(RUNNER_IMAGES_PREPARE),build)
	$(MAKE) runner-images-build
else ifeq ($(RUNNER_IMAGES_PREPARE),skip)
	@echo "skipping runner image preparation"
else
	$(MAKE) runner-images-pull
endif

runner-images-pull:
	@for dir in deploy/languages/*/; do \
		slug=$$(basename $$dir); \
		[ -f "$$dir/Dockerfile" ] || continue; \
		docker pull $(RUNNER_IMAGE_REGISTRY)/soj-runner-$$slug:$(RUNNER_IMAGE_TAG); \
	done

runner-images-build:
	@for dir in deploy/languages/*/; do \
		slug=$$(basename $$dir); \
		[ -f "$$dir/Dockerfile" ] || continue; \
		docker build -f "$$dir/Dockerfile" -t $(RUNNER_IMAGE_REGISTRY)/soj-runner-$$slug:$(RUNNER_IMAGE_TAG) "$$dir"; \
	done

up:
	docker compose -f $(COMPOSE_FILE) up --build -d

smoke:
	./deploy/smoke.sh

smoke-real-docker: runner-images
	mkdir -p $(SOJ_DOCKER_RUNNER_WORKDIR)
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	docker compose down -v --remove-orphans
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	docker compose up --build -d
	COMPOSE_FILES=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SMOKE_REAL_JUDGE=1 \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	./deploy/smoke.sh

smoke-real-gvisor: runner-images
	mkdir -p $(SOJ_DOCKER_RUNNER_WORKDIR)
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	./scripts/dev/check-docker-runner.sh
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	docker compose down -v --remove-orphans
	COMPOSE_FILE=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=runsc \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
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
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	docker compose down -v --remove-orphans
	COMPOSE_FILES=$(DOCKER_RUNNER_COMPOSE_FILES) \
	SOJ_ENV=local \
	SOJ_DOCKER_RUNNER_RUNTIME=$(SOJ_DOCKER_RUNNER_RUNTIME) \
	SOJ_DOCKER_RUNNER_WORKDIR=$(SOJ_DOCKER_RUNNER_WORKDIR) \
	SOJ_RUNNER_REGISTRY=$(RUNNER_IMAGE_REGISTRY) \
	SOJ_RUNNER_TAG=$(RUNNER_IMAGE_TAG) \
	./scripts/dev/runner-capacity-smoke.sh

down:
	docker compose -f $(COMPOSE_FILE) down -v --remove-orphans
