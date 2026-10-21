ARG BASE_IMAGE
FROM ${BASE_IMAGE}
ARG RUN_OWNER
LABEL com.factorybox.run=${RUN_OWNER}
ARG TEST_GOPROXY=https://proxy.golang.org,direct
ARG TEST_GOSUMDB=sum.golang.org
ENV GOMAXPROCS=2 GOMEMLIMIT=512MiB GOFLAGS=-p=2 GOPROXY=${TEST_GOPROXY} GOSUMDB=${TEST_GOSUMDB}
COPY candidate /src/sysbox-libs/linuxUtils
COPY upstream-tests.sha256 /tmp/upstream-tests.sha256
RUN sha256sum -c /tmp/upstream-tests.sha256 > /out/fs-original-test-checks.txt
WORKDIR /src/sysbox-fs
RUN --mount=type=cache,id=factorybox-go-mod-shared,target=/root/go/pkg/mod \
    --mount=type=cache,id=factorybox-go-build-shared,target=/root/.cache/go-build \
    set -eu; \
    go mod verify; \
    mkdir -p /out/fs-unit; \
    go list -mod=readonly -f '{{.ImportPath}}|{{.Dir}}' ./... > /out/fs-all-packages.txt; \
    : > /out/fs-test-packages.txt; \
    index=0; \
    while IFS='|' read -r package directory; do \
      index=$((index + 1)); \
      (cd "$directory" && go test -mod=readonly -c -trimpath -buildvcs=false \
        -o "/out/fs-unit/$index.test" .); \
      if test -f "/out/fs-unit/$index.test"; then \
        printf '%s|%s|%s\n' "$package" "$directory" "$index" >> /out/fs-test-packages.txt; \
      fi; \
    done < /out/fs-all-packages.txt; \
    sha256sum /out/fs-unit/*.test > /out/fs-unit-binaries.sha256; \
    CGO_ENABLED=1 go build -mod=readonly -trimpath -buildvcs=false \
      -tags 'netgo osusergo' \
      -ldflags '-extldflags -static -X main.version=0.7.1-experiment -X main.commitId=c3d2ebc65102e32e74e383675f03b45556326888-experiment' \
      -o /out/sysbox-fs-candidate ./cmd/sysbox-fs
