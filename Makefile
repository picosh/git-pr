fmt:
	go fmt ./...
	deno fmt README.md
.PHONY: fmt

lint:
	golangci-lint run -E goimports -E godot --timeout 10m
.PHONY: lint

build:
	go build -o ./build/git ./cmd/git
.PHONY: build
