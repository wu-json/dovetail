set shell := ["bash", "-cu"]

version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
image := "dovetail"

# Build the dovetail binary
build:
    mkdir -p dist
    go build -ldflags "-X main.version={{version}}" -o dist/dovetail ./cmd/dovetail

# Build multi-arch Docker image (amd64 + arm64)
docker-build:
    docker buildx build --platform linux/amd64,linux/arm64 \
        --build-arg VERSION={{version}} \
        -t {{image}}:{{version}} \
        -t {{image}}:latest \
        .

# Build and push multi-arch Docker image
docker-push registry:
    docker buildx build --platform linux/amd64,linux/arm64 \
        --build-arg VERSION={{version}} \
        -t {{registry}}/{{image}}:{{version}} \
        -t {{registry}}/{{image}}:latest \
        --push \
        .
