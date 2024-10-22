DOCKER_TAG?=$(shell git log --format="%H" -n 1)
DOCKER_PLATFORM?=linux/amd64,linux/arm64
DOCKER_CMD?=docker
DOCKER_BUILDX_BUILD?=$(DOCKER_CMD) buildx build --push --platform $(DOCKER_PLATFORM)

fmt:
	go fmt ./...
	deno fmt README.md
.PHONY: fmt

lint:
	golangci-lint run -E goimports -E godot --timeout 10m
.PHONY: lint

test:
	go test ./...
.PHONY: test

build:
	go build -o ./build/ssh ./cmd/ssh
	go build -o ./build/web ./cmd/web
.PHONY: build

bp-setup:
	$(DOCKER_CMD) buildx ls | grep pico || $(DOCKER_CMD) buildx create --name pico
	$(DOCKER_CMD) buildx use pico
.PHONY: bp-setup

bp: bp-setup
	$(DOCKER_BUILDX_BUILD) -t "ghcr.io/picosh/pico/git-ssh:$(DOCKER_TAG)" --target release-ssh .
	$(DOCKER_BUILDX_BUILD) -t "ghcr.io/picosh/pico/git-web:$(DOCKER_TAG)" --target release-web .
.PHONY: bp

deploy: bp
	ssh ppipe pub git-pr-deploy -e
.PHONY: deploy

smol:
	curl https://pico.sh/smol.css -o ./static/smol.css
.PHONY: smol
