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

FROM quay.io/centos/centos:stream10

WORKDIR /

RUN dnf install -y iproute iproute-tc catatonit && dnf clean all
RUN ln -s /usr/libexec/podman/catatonit /sbin/tini

RUN mkdir /rpms
ADD https://github.com/DPDK/grout/releases/download/v0.15.0/frr.x86_64.rpm /tmp/
ADD https://github.com/DPDK/grout/releases/download/v0.15.0/grout-frr.x86_64.rpm /tmp/
ADD https://github.com/DPDK/grout/releases/download/v0.15.0/grout.x86_64.rpm /tmp/

RUN dnf install -y /tmp/*.rpm && dnf clean all && rm -rf /tmp/*.rpm

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

RUN ln -s /usr/libexec/frr /usr/lib/frr
ADD https://raw.githubusercontent.com/FRRouting/frr/refs/tags/frr-10.5.0/docker/ubi8-minimal/docker-start /usr/lib/frr/docker-start
RUN chmod +x /usr/lib/frr/docker-start


COPY --from=quay.io/grout/grout /usr/bin/grcli /usr/bin/grcli
COPY --from=quay.io/grout/grout /lib64/libedit.so.0 /lib64/libedit.so.0
COPY --from=quay.io/grout/grout /lib64/libedit.so.0.0.72 /lib64/libedit.so.0.0.72
ENTRYPOINT ["/controller"]
