.PHONY: build run clean test lint

BINARY=dovetail
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS=-ldflags "-X main.version=$(VERSION)"

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/dovetail

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY)
	go clean

test:
	go test -v ./...

lint:
	golangci-lint run

docker-build:
	docker build -t dovetail:latest .

docker-run:
	docker run --rm -it \
		-v /var/run/docker.sock:/var/run/docker.sock:ro \
		-v dovetail-state:/var/lib/dovetail \
		-e TS_AUTHKEY \
		dovetail:latest
