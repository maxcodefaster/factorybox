FROM golang@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80 AS go
FROM ghcr.io/coder/envbox@sha256:13e4477ed65912a72e52bb7115db63d4da934cf8a55962c8fdf7c470901dceff AS build
USER root
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:/root/go/bin:$PATH
ENV GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local
ARG FACTORYBOX_BUILD_CACHE=shared
RUN --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    rm -f /etc/apt/sources.list.d/* \
    && printf '%s\n' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy main restricted universe multiverse' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy-updates main restricted universe multiverse' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy-security main restricted universe multiverse' \
      > /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends \
      build-essential pkg-config libseccomp-dev patch protobuf-compiler libprotobuf-dev \
    && rm -rf /var/lib/apt/lists/* && mkdir -p /out
COPY sysbox-runc /src/sysbox-runc
COPY sysbox-ipc /src/sysbox-ipc
COPY sysbox-libs /src/sysbox-libs
COPY sysbox-fs /src/sysbox-fs
COPY sysbox-mgr /src/sysbox-mgr
COPY sysbox-runc-pristine.tar.gz /tmp/runc.tar.gz
WORKDIR /src/sysbox-runc
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    go version > /out/toolchain.txt \
    && cc --version >> /out/toolchain.txt \
    && dpkg-query -W libseccomp-dev protobuf-compiler libprotobuf-dev >> /out/toolchain.txt \
    && dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\n' > /out/packages.tsv \
    && cp /etc/apt/sources.list /out/apt-sources.list \
    && go install github.com/golang/protobuf/protoc-gen-go@v1.5.4 \
    && make -C /src/sysbox-ipc
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    go test -mod=readonly ./libsysbox/syscont \
      -run '^(TestCfgSeccomp|TestExperimentSeccompBlacklist)' -count=1 \
    && go test -mod=readonly ./libcontainer/cgroups/fs2 -count=1 \
    && go test -mod=readonly -tags 'seccomp apparmor idmapped_mnt netgo osusergo' . \
      -run '^(TestExperiment(CopyPipeline|Lineage)|TestFactorybox(FsSnapshotScope|Restore|Resume))' -count=1 \
    && go test -mod=readonly -tags 'seccomp apparmor idmapped_mnt netgo osusergo' ./libcontainer \
      -run '^TestFactoryboxRestoreCgroup$' -count=1
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    go test -mod=readonly -c -tags 'seccomp apparmor idmapped_mnt netgo osusergo' \
      -o /out/libcontainer.test ./libcontainer \
    && go test -mod=readonly -c -tags 'seccomp apparmor idmapped_mnt netgo osusergo' \
      -o /out/integration.test ./libcontainer/integration
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    CGO_ENABLED=1 go build -mod=readonly -buildvcs=false -trimpath \
      -tags 'seccomp apparmor idmapped_mnt netgo osusergo' \
      -ldflags '-extldflags -static -X main.version=0.7.1-experiment -X main.commitId=081856cc5d17e7095f066b08d0eca6bb0b515c47-experiment' \
      -o /out/sysbox-runc .
# Sysbox-fs imports the upstream libcontainer under its nestybox module path.
# The private CLI command must not enter that dependency through a second module identity.
RUN rm -rf /src/sysbox-runc && mkdir /src/sysbox-runc \
    && tar -xzf /tmp/runc.tar.gz --strip-components=1 -C /src/sysbox-runc
WORKDIR /src/sysbox-fs
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    go test -mod=readonly ./state -run '^TestExperiment(Retained|Fork)' -count=1 \
    && go test -mod=readonly ./state -run '^TestFactoryboxSnapshotRejectsMalformedState$' -count=1 \
    && go test -mod=readonly ./ipc -run '^TestContainerPreRegister$' -count=1 \
    && CGO_ENABLED=1 go build -mod=readonly -buildvcs=false -trimpath \
      -tags 'netgo osusergo' \
      -ldflags '-extldflags -static -X main.version=0.7.1-experiment -X main.commitId=c3d2ebc65102e32e74e383675f03b45556326888-experiment' \
      -o /out/sysbox-fs ./cmd/sysbox-fs
WORKDIR /src/sysbox-mgr
RUN --mount=type=cache,id=factorybox-go-mod-${FACTORYBOX_BUILD_CACHE},target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-${FACTORYBOX_BUILD_CACHE},target=/root/.cache/go-build \
    --mount=type=bind,source=build-resolv.conf,target=/etc/resolv.conf,readonly \
    go test -mod=readonly -tags idmapped_mnt . -run '^TestFactoryboxBinfmtMiscPolicy$' -count=1 \
    && CGO_ENABLED=1 go build -mod=readonly -buildvcs=false -trimpath \
      -tags 'idmapped_mnt netgo osusergo' \
      -ldflags '-extldflags -static -X main.version=0.7.1-experiment -X main.commitId=2d45af01bcfd8641e6641f76104c6003bf501cd7' \
      -o /out/sysbox-mgr .
FROM scratch
COPY --from=build /out/ /
