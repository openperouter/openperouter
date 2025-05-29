# Build the manager binary
FROM golang:1.24.3 AS builder

ARG GIT_COMMIT=dev
ARG GIT_BRANCH=dev
ARG TARGETOS
ARG TARGETARCH

WORKDIR $GOPATH/openperouter
# Cache the downloads
COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/go/pkg \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o reloader ./cmd/reloader \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o controller ./cmd/hostcontroller \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o cp-tool ./cmd/cp-tool \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o nodemarker ./cmd/nodemarker \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o operator ./cmd/operator

FROM gcr.io/distroless/static:latest
WORKDIR /
COPY --from=builder /go/openperouter/reloader .
COPY --from=builder /go/openperouter/controller .
COPY --from=builder /go/openperouter/cp-tool .
COPY --from=builder /go/openperouter/nodemarker .
COPY --from=builder /go/openperouter/operator .
COPY bindata bindata

ENTRYPOINT ["/controller"]
