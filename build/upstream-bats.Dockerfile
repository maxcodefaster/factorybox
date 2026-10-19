ARG NATIVE_BASE_IMAGE
FROM ${NATIVE_BASE_IMAGE}
USER root
# The verified native base already contains the signed, dated APT source list.
RUN apt-get update \
    && apt-get install -y --no-install-recommends bats psmisc lsof tini \
    && dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\n' \
      > /opt/factorybox-bats-packages.tsv \
    && rm -rf /var/lib/apt/lists/*
