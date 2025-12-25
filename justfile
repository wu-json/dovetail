set shell := ["bash", "-cu"]

version := `svu current --strip-prefix 2>/dev/null || echo "0.0.0"`
image := "dovetail"

# Show current version
current-version:
    @echo {{version}}

# Set version to a specific semver
version semver:
    echo "{{semver}}" > VERSION

# Bump version, commit, and tag
bump-and-commit-version bump_type:
    #!/usr/bin/env bash
    new_version=$(svu {{bump_type}} --strip-prefix)
    just version $new_version
    git add -A
    git commit -m "chore(release): v$new_version"
    git tag -a v$new_version -m "Release v$new_version"
    git push --follow-tags

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
