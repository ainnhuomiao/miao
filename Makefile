BINARY := bin/miao

.PHONY: run build fmt fmt-check vet test race lint check clean

run:
	go run ./cmd/miao

build:
	go build -trimpath -o $(BINARY) ./cmd/miao

fmt:
	gofmt -w cmd internal

fmt-check:
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "未 gofmt：$$out"; exit 1; fi

vet:
	go vet ./...

test:
	go test ./...

race:
	CGO_ENABLED=1 go test -race ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then golangci-lint run; \
	else echo "未安装 golangci-lint，可用 nix develop 提供"; fi

check: fmt-check build vet test race lint

clean:
	rm -f $(BINARY)
