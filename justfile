set shell := ["bash", "-cu"]

version := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`

# Build the dovetail binary
build:
    mkdir -p dist
    go build -ldflags "-X main.version={{version}}" -o dist/dovetail ./cmd/dovetail
