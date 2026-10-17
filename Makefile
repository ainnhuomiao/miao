BINARY := bin/miao

.PHONY: run build fmt fmt-check vet test race check clean

run:
	go run ./cmd/miao

build:
	go build -trimpath -o $(BINARY) ./cmd/miao

fmt:
	go fmt ./...

vet:
	go vet ./...

test:
	go test ./...

fmt-check:
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "以下文件未 gofmt："; echo "$$out"; exit 1; fi

# -race 需要 cgo：有 gcc 直接跑，没有就进 nix-shell 跑
race:
	@if command -v gcc >/dev/null 2>&1; then CGO_ENABLED=1 go test -race ./...; \
	else nix-shell -p gcc go --run 'CGO_ENABLED=1 go test -race ./...'; fi

# 提交前跑：格式、编译、vet、测试、竞态测试；不会改动任何文件
check: fmt-check build vet test race

clean:
	rm -rf bin
