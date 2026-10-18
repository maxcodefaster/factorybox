ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base
ARG MODULE_DIR=/src/sysbox-ipc
COPY . ${MODULE_DIR}/
WORKDIR ${MODULE_DIR}
ENV GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local FACTORYBOX_UPSTREAM_CONTAINER=1

FROM base AS readonly
RUN --mount=type=cache,id=factorybox-go-mod,target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build,target=/root/.cache/go-build \
    go test -mod=readonly -count=1 ./...

FROM base AS tidy
RUN --mount=type=cache,id=factorybox-go-mod,target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build,target=/root/.cache/go-build \
    mkdir /module-proof \
    && cp go.mod /module-proof/go.mod.before \
    && cp go.sum /module-proof/go.sum.before \
    && go mod tidy \
    && cp go.mod /module-proof/go.mod.after \
    && cp go.sum /module-proof/go.sum.after

FROM scratch AS module-output
COPY --from=tidy /module-proof/ /
