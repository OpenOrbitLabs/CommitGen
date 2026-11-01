VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test vet install release-snapshot clean

build:
	go build -ldflags "$(LDFLAGS)" -o commitgen .

test:
	go test ./...

vet:
	go vet ./...

install:
	go install -ldflags "$(LDFLAGS)" .

release-snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf commitgen commitgen.exe dist
