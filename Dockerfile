FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS builder

WORKDIR /app

# Install git for version info
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build args for cross-compilation
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags "-X main.version=${VERSION}" -o dovetail ./cmd/dovetail

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

COPY --from=builder /app/dovetail /usr/local/bin/dovetail

# Create state directory
RUN mkdir -p /var/lib/dovetail

ENTRYPOINT ["/usr/local/bin/dovetail"]
