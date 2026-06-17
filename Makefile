.PHONY: build test test-integration lint clean

build:
	go build -o bin/fleet-scan ./cmd/fleet-scan

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	go vet ./...

clean:
	rm -rf bin/
