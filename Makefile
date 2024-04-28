fmt:
	go fmt ./...
.PHONY: fmt

build:
	go build -o ./build/git ./cmd/git
.PHONY: build
