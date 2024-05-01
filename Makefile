fmt:
	go fmt ./...
	deno fmt README.md
.PHONY: fmt

build:
	go build -o ./build/git ./cmd/git
.PHONY: build
