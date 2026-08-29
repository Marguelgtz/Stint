.PHONY: test vet build run fmt check

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o bin/stint ./cmd/stint

run:
	go run ./cmd/stint

fmt:
	gofmt -w cmd internal

check: fmt vet test build
