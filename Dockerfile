ARG FRR_IMAGE=quay.io/frrouting/frr:10.6.0

# Build the manager binary
FROM golang:1.26.4 AS builder

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
COPY config/ config/

RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/go/pkg/mod \
  --mount=type=bind,source=go.sum,target=go.sum \
  --mount=type=bind,source=go.mod,target=go.mod \
  --mount=type=bind,source=internal,target=internal \
  --mount=type=bind,source=api,target=api \
  --mount=type=bind,source=cmd,target=cmd \
  --mount=type=bind,source=operator,target=operator \
  --mount=type=bind,source=config,target=config \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o reloader ./cmd/reloader \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o controller ./cmd/hostcontroller \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o nodemarker ./cmd/nodemarker \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o hostbridge ./cmd/hostbridge \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o operatorbinary ./operator

# Build the CNI plugins the controller invokes to provision CNI-backed
# underlay interfaces. Statically linked (CGO_ENABLED=0) since the final
# image is musl based.
FROM golang:1.26.4 AS cni-plugins

ARG TARGETOS
ARG TARGETARCH
ARG CNI_PLUGINS_VERSION=v1.7.1

RUN git clone --depth 1 --branch ${CNI_PLUGINS_VERSION} \
  https://github.com/containernetworking/plugins /go/cni-plugins
WORKDIR /go/cni-plugins
RUN --mount=type=cache,target=/root/.cache/go-build \
  --mount=type=cache,target=/go/pkg/mod \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o /cni/bin/macvlan ./plugins/main/macvlan \
  && \
  CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -v -o /cni/bin/static ./plugins/ipam/static

FROM ${FRR_IMAGE}
WORKDIR /
COPY --from=builder /go/openperouter/reloader .
COPY --from=builder /go/openperouter/controller .
COPY --from=builder /go/openperouter/hostbridge .
COPY --from=builder /go/openperouter/nodemarker .
COPY --from=builder /go/openperouter/operatorbinary ./operator
COPY --from=cni-plugins /cni/bin/ /opt/openperouter/cni/bin/
COPY operator/bindata bindata
# Copy FRR startup configuration to the default location
COPY systemdmode/frrconfig/daemons /etc/frr/daemons
COPY systemdmode/frrconfig/vtysh.conf /etc/frr/vtysh.conf
COPY systemdmode/frrconfig/frr.conf /etc/frr/frr.conf

ENTRYPOINT ["/controller"]
