ARG FRR_IMAGE=quay.io/frrouting/frr:10.6.0
ARG CNI_PLUGINS_VERSION=v1.6.2

# FRR_VARIANT selects the FRR runtime baked into the router image:
#   release (default) -> the stock, stripped FRR from ${FRR_IMAGE}
#   debug             -> FRR rebuilt from source with debug info, frame
#                        pointers and libunwind, plus gdb, so that a crashing
#                        daemon prints a usable backtrace and the collected
#                        core dumps can be symbolized.
# Only "debug" is expensive to build, and it is meant for debugging CI crashes.
ARG FRR_VARIANT=release
ARG FRR_REF=frr-10.6.0
ARG FRR_ALPINE_VERSION=3.22

# Build CNI plugin binaries
FROM golang:1.26.4 AS cni-plugins-builder

ARG CNI_PLUGINS_VERSION
ARG TARGETOS
ARG TARGETARCH

WORKDIR /cni-plugins
RUN git clone --branch ${CNI_PLUGINS_VERSION} --depth 1 https://github.com/containernetworking/plugins.git .
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} ./build_linux.sh \
  -ldflags "-extldflags -static"

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

# Rebuild FRR from source with debug information. Only used when
# FRR_VARIANT=debug, docker skips this stage otherwise.
#
# The configure flags mirror the ones the stock image reports in
# "zebra --version", plus --enable-libunwind: without it FRR cannot print a
# backtrace on musl, which is why the crash handler only logs
# "Received signal 6 ... aborting..." with no frames.
FROM alpine:${FRR_ALPINE_VERSION} AS frr-source-build

ARG FRR_REF
ARG FRR_CONFIGURE_EXTRA=""

RUN apk add --no-cache \
  autoconf \
  automake \
  bison \
  build-base \
  c-ares-dev \
  elfutils-dev \
  flex \
  git \
  json-c-dev \
  libcap-dev \
  libtool \
  libunwind-dev \
  libyang-dev \
  linux-headers \
  lua5.3-dev \
  ncurses-dev \
  openssl-dev \
  patch \
  pcre2-dev \
  perl \
  pkgconfig \
  protobuf-c-dev \
  py3-pip \
  python3-dev \
  readline-dev \
  rtrlib-dev \
  texinfo

WORKDIR /src
RUN git clone --depth 1 --branch ${FRR_REF} https://github.com/FRRouting/frr.git .

# Apply the FRR patches under test. They are vendored rather than fetched from
# GitHub so that the build is reproducible and a reviewer can see exactly what
# went into the image.
COPY hack/debug/frr-patches /frr-patches
RUN for patch in /frr-patches/*.patch; do \
  [ -e "$patch" ] || continue; \
  echo "applying $patch"; \
  git apply --verbose "$patch"; \
  done

# Record what actually went into this image. Not knowing which patches a
# router image carries has already cost us one wrong conclusion, so the answer
# travels with the binary and can be read back with
# "docker run --entrypoint cat <image> /etc/frr-build-info".
RUN { \
  echo "frr_ref=${FRR_REF}"; \
  echo "frr_commit=$(git rev-parse HEAD)"; \
  echo "frr_describe=$(git describe --always --tags 2>/dev/null || echo unknown)"; \
  echo "built=$(date -u +%Y-%m-%dT%H:%M:%SZ)"; \
  for patch in /frr-patches/*.patch; do \
  [ -e "$patch" ] || continue; \
  echo "patch=$(basename "$patch") sha256=$(sha256sum "$patch" | cut -d' ' -f1)"; \
  done; \
  } > /frr-build-info

RUN ./bootstrap.sh && \
  ./configure \
  --prefix=/usr \
  --sysconfdir=/etc \
  --localstatedir=/var \
  --sbindir=/usr/lib/frr \
  --libdir=/usr/lib \
  --enable-rpki \
  --enable-vtysh \
  --enable-multipath=64 \
  --enable-vty-group=frrvty \
  --enable-user=frr \
  --enable-group=frr \
  --enable-pcre2posix \
  --enable-scripting \
  --enable-libunwind \
  ${FRR_CONFIGURE_EXTRA} \
  CFLAGS="-g3 -Og -fno-omit-frame-pointer" && \
  make -j"$(nproc)" && \
  make install DESTDIR=/frr-install

# Keep only the ELF artifacts. The python/shell helpers (frr-reload.py in
# particular) are left untouched so that the patch applied below keeps
# matching the file shipped by the stock image.
RUN mkdir -p /frr-debug-out/usr/lib/frr /frr-debug-out/usr/lib /frr-debug-out/usr/bin && \
  find /frr-install/usr/lib/frr -maxdepth 1 -type f -perm -u+x \
  ! -name '*.py' ! -name '*.sh' \
  -exec cp -a {} /frr-debug-out/usr/lib/frr/ \; && \
  cp -a /frr-install/usr/lib/libfrr*.so* /frr-debug-out/usr/lib/ && \
  cp -a /frr-install/usr/lib/libmlag_pb*.so* /frr-debug-out/usr/lib/ && \
  cp -a /frr-install/usr/bin/vtysh /frr-debug-out/usr/bin/vtysh && \
  cp -a /usr/lib/libyang.so* /frr-debug-out/usr/lib/ && \
  mkdir -p /frr-debug-out/etc && \
  cp -a /frr-build-info /frr-debug-out/etc/frr-build-info

FROM ${FRR_IMAGE} AS frr-release

FROM ${FRR_IMAGE} AS frr-debug
# The daemons, libfrr and libyang all come from the source build: the stock
# image pins an older libyang than the one the build links against
# (lyd_trim_xpath is missing in 2.1.128), so they have to travel together.
# gdb symbolizes the collected core dumps in place, valgrind can be turned on
# per daemon through the zebra_wrap hook in the daemons file when the crash has
# to be caught at the point of the corruption instead of at the assert.
# AddressSanitizer is not an option here: Alpine ships no libasan for musl.
COPY --from=frr-source-build /frr-debug-out/ /
RUN apk update && apk add --no-cache gdb libunwind valgrind

FROM frr-${FRR_VARIANT}
WORKDIR /
COPY --from=builder /go/openperouter/reloader .
COPY --from=builder /go/openperouter/controller .
COPY --from=builder /go/openperouter/hostbridge .
COPY --from=builder /go/openperouter/nodemarker .
COPY --from=builder /go/openperouter/operatorbinary ./operator
COPY operator/bindata bindata
COPY --from=cni-plugins-builder /cni-plugins/bin/macvlan /opt/openperouter/cni/bin/
COPY --from=cni-plugins-builder /cni-plugins/bin/ipvlan /opt/openperouter/cni/bin/
COPY --from=cni-plugins-builder /cni-plugins/bin/static /opt/openperouter/cni/bin/
COPY --from=cni-plugins-builder /cni-plugins/bin/dhcp /opt/openperouter/cni/bin/
# Copy FRR startup configuration to the default location
COPY systemdmode/frrconfig/daemons /etc/frr/daemons
COPY systemdmode/frrconfig/vtysh.conf /etc/frr/vtysh.conf
COPY systemdmode/frrconfig/frr.conf /etc/frr/frr.conf

# Hack for https://github.com/FRRouting/frr/issues/20355
#          https://github.com/FRRouting/frr/pull/20378 
COPY 0001-Revert-tools-Allow-deleting-of-interfaces.patch .
RUN apk update && apk add patch
RUN patch /usr/lib/frr/frr-reload.py 0001-Revert-tools-Allow-deleting-of-interfaces.patch

ENTRYPOINT ["/controller"]
