.PHONY: check structure test vet build fmt

check: structure test vet

structure:
	python3 scripts/check_structure.py

test:
	GOWORK=off go test ./...
	GOWORK=off go -C packages/mt4 test ./...
	GOWORK=off go -C research/mt4-wire test ./...
	python3 -m unittest discover -s tests/capture -v

vet:
	GOWORK=off go vet ./...
	GOWORK=off go -C packages/mt4 vet ./...
	GOWORK=off go -C research/mt4-wire vet ./...

build:
	mkdir -p .local/bin
	GOWORK=off go build -o .local/bin/mt4probe ./cmd/mt4probe
	GOWORK=off go -C packages/mt4 build -o ../../.local/bin/mt4verify ./tests/cmd/verify

fmt:
	gofmt -w cmd internal packages/mt4 research tests
