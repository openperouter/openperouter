ARG FRR_IMAGE=quay.io/frrouting/frr:10.2.1

# Build the manager binary
FROM golang:1.25.7 AS builder

ARG GIT_COMMIT=dev
ARG GIT_BRANCH=dev
ARG TARGETOS
ARG TARGETARCH

WORKDIR $GOPATH/openperouter
RUN --mount=type=cache,target=/go/pkg/mod/ \
  --mount=type=bind,source=go.sum,target=go.sum \
  --mount=type=bind,source=go.mod,target=go.mod \
  go mod download -x

COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/
COPY operator/ operator/

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,source=go.sum,target=go.sum \
  --mount=type=bind,source=go.mod,target=go.mod \
  --mount=type=bind,source=internal,target=internal \
  --mount=type=bind,source=api,target=api \
  --mount=type=bind,source=cmd,target=cmd \
  --mount=type=bind,source=operator,target=operator \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o reloader ./cmd/reloader \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o controller ./cmd/hostcontroller \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o nodemarker ./cmd/nodemarker \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o hostbridge ./cmd/hostbridge \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o operatorbinary ./operator

FROM ${FRR_IMAGE}
WORKDIR /
COPY --from=builder /go/openperouter/reloader .
COPY --from=builder /go/openperouter/controller .
COPY --from=builder /go/openperouter/hostbridge .
COPY --from=builder /go/openperouter/nodemarker .
COPY --from=builder /go/openperouter/operatorbinary ./operator
COPY operator/bindata bindata
# Copy FRR startup configuration to the default location
COPY systemdmode/frrconfig/daemons /etc/frr/daemons
COPY systemdmode/frrconfig/vtysh.conf /etc/frr/vtysh.conf
COPY systemdmode/frrconfig/frr.conf /etc/frr/frr.conf

ENTRYPOINT ["/controller"]
