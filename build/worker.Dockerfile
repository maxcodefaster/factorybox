ARG NATIVE_BASE
FROM ${NATIVE_BASE}
ARG SOURCE_DATE_EPOCH=0
ARG FACTORYBOX_VERSION
USER root
COPY --chmod=0700 bundle/ /opt/factorybox/
RUN chmod 0755 /opt/factorybox/factorybox /opt/factorybox/components/* \
    && chmod 0600 /opt/factorybox/components.json /opt/factorybox/build.json
LABEL io.factorybox.worker="unselected-candidate"
LABEL org.opencontainers.image.title="Factorybox worker" \
      org.opencontainers.image.description="Privileged Linux system-container lifecycle worker with pinned Sysbox and CRIU; shares the host kernel" \
      org.opencontainers.image.source="https://github.com/maxcodefaster/factorybox" \
      org.opencontainers.image.url="https://github.com/maxcodefaster/factorybox" \
      org.opencontainers.image.version="${FACTORYBOX_VERSION}"
STOPSIGNAL SIGTERM
ENTRYPOINT ["/opt/factorybox/factorybox", "worker"]
CMD ["--store", "/var/lib/sysbox/store", "--components", "/opt/factorybox/components", "--manifest", "/opt/factorybox/components.json"]
