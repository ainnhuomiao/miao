BINARY := bin/agent

.PHONY: run build fmt vet test check clean

run:
	go run ./cmd/agent

build:
	go build -trimpath -o $(BINARY) ./cmd/agent

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

check: fmt vet test

clean:
	rm -rf bin
