.PHONY: test vet build build-release run fmt check

test:
	go test ./...

vet:
	go vet ./...

build:
	go build -o bin/stint ./cmd/stint

build-release:
	@test -n "$(VERSION)" || (echo "VERSION is required (for example VERSION=0.0.1)" >&2; exit 1)
	go build -ldflags "-X main.version=$(VERSION)" -o bin/stint ./cmd/stint

run:
	go run ./cmd/stint

fmt:
	gofmt -w cmd internal

check: fmt vet test build
