# syntax=docker/dockerfile:1
# Multi-arch image: the build stage runs on the builder's platform and
# cross-compiles for the target (linux/amd64, linux/arm64), so no emulation
# is needed. The runtime image is distroless, non-root, with no shell.
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS build
ENV GOTOOLCHAIN=local CGO_ENABLED=0
WORKDIR /src
COPY go.mod go.sum ./
COPY sdk/go.mod sdk/go.sum ./sdk/
RUN go mod download
COPY . .
ARG VERSION=dev
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/compliance-engine ./cmd/compliance-engine

FROM gcr.io/distroless/static-debian12:nonroot
ARG VERSION=dev
LABEL org.opencontainers.image.title="compliance-engine" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.description="Data-source-agnostic compliance engine (open core)"
COPY --from=build /out/compliance-engine /usr/local/bin/compliance-engine
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/compliance-engine"]
CMD ["serve"]
