# Keep dependency acquisition in the bounded builder. Native tests run offline.
ARG BUILDER_IMAGE
FROM ${BUILDER_IMAGE}
COPY . /factorybox-input
RUN --mount=type=cache,id=factorybox-go-mod-shared,target=/factorybox-mod-cache \
    mkdir -p /root/go/pkg/mod \
    && cp -a /factorybox-mod-cache/. /root/go/pkg/mod/ \
    && for module in /factorybox-input/*/go.mod /factorybox-input/sysbox-libs/*/go.mod; do \
      (cd "$(dirname "$module")" && go mod download); \
    done
