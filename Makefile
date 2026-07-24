.PHONY: build deploy test test-integration lint clean

BINARY := bin/fleet-scan
INSTALL := $(HOME)/bin/fleet-scan

build:
	go build -o $(BINARY) ./cmd/fleet-scan

test:
	go test ./...

test-integration:
	go test -tags=integration ./...

lint:
	go vet ./...

clean:
	rm -rf bin/

deploy: build
	mkdir -p $(HOME)/bin
	ln -sf $(CURDIR)/$(BINARY) $(INSTALL)
	@echo ""
	@echo "# Built version"
	$(BINARY) version
	@echo ""
	@echo "# Locally available version"
	fleet-scan version
