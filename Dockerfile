# Build the golinkname binary.
FROM golang:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests first so the dep download layer is
# cached and not invalidated by source changes.
COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter).
COPY . .

# Build. GOARCH has no default value so the binary matches the host
# architecture when invoked locally; in CI buildx fills TARGETARCH per
# platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
    -a \
    -o golinkname \
    ./cmd/golinkname

# Use distroless as a minimal base image to package the CLI.
# Refer to https://github.com/GoogleContainerTools/distroless for details.
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/golinkname .
COPY --from=builder /workspace/LICENSE /LICENSE
COPY --from=builder /workspace/LICENSE-third-party /LICENSE-third-party
USER 65532:65532

ENTRYPOINT ["/golinkname"]
