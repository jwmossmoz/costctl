BINARY := costctl
PKG    := github.com/jwmossmoz/costctl
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG)/cmd.Version=$(VERSION)

.PHONY: build install test vet fmt clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w -s .

clean:
	rm -f $(BINARY)
