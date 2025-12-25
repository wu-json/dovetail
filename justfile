set shell := ["bash", "-cu"]

version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
image := "dovetail"

build:
    mkdir -p dist
    go build -ldflags "-X main.version={{version}}" -o dist/dovetail ./cmd/dovetail

run: build
    ./dist/dovetail

test:
    go test -v ./...

lint:
    golangci-lint run

clean:
    rm -rf dist
    go clean

docker-build:
    docker buildx build --platform linux/amd64,linux/arm64 \
        --build-arg VERSION={{version}} \
        -t {{image}}:{{version}} \
        -t {{image}}:latest \
        .

docker-push registry:
    docker buildx build --platform linux/amd64,linux/arm64 \
        --build-arg VERSION={{version}} \
        -t {{registry}}/{{image}}:{{version}} \
        -t {{registry}}/{{image}}:latest \
        --push \
        .

docker-run:
    docker run --rm -it \
        -v /var/run/docker.sock:/var/run/docker.sock:ro \
        -v dovetail-state:/var/lib/dovetail \
        -e TS_AUTHKEY \
        {{image}}:latest
