VERSION ?= dev
BINARY := dist/mywhoosh
LDFLAGS := -s -w -X github.com/voydz/mywhoosh-cli/internal/cli.Version=$(VERSION)

.PHONY: build test lint check package smoke

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/mywhoosh

test:
	go test -race ./...

lint:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; exit 1; }
	go vet ./...

check: lint test build smoke

smoke:
	$(BINARY) version
	$(BINARY) --help

package: build
	bash packaging/package.sh '$(VERSION)'
