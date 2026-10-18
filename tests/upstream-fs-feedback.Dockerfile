ARG BASE_IMAGE
FROM ${BASE_IMAGE} AS base
COPY . /src/sysbox-fs/
WORKDIR /src/sysbox-fs
ENV GOMAXPROCS=2 GOFLAGS=-p=2 GOTOOLCHAIN=local

FROM base AS registration
RUN --mount=type=cache,id=factorybox-go-mod,target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build,target=/root/.cache/go-build \
    go test -mod=readonly -count=1 -v ./state \
      -run '^Test_containerStateService_ContainerRegister$'

FROM base AS handler
RUN --mount=type=cache,id=factorybox-go-mod,target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build,target=/root/.cache/go-build \
    go test -mod=readonly -count=1 -v ./handler/implementations \
      -run '^TestPassThrough_Lookup$'
