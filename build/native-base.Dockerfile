FROM ghcr.io/coder/envbox@sha256:13e4477ed65912a72e52bb7115db63d4da934cf8a55962c8fdf7c470901dceff
USER root
LABEL io.factorybox.native-base="1"
RUN rm -f /etc/apt/sources.list.d/* \
    && printf '%s\n' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy main restricted universe multiverse' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy-updates main restricted universe multiverse' \
      'deb [check-valid-until=no] https://snapshot.ubuntu.com/ubuntu/20261018T000000Z jammy-security main restricted universe multiverse' \
      > /etc/apt/sources.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
      build-essential pkg-config libprotobuf-dev libprotobuf-c-dev protobuf-c-compiler protobuf-compiler \
      libnl-3-dev libnl-route-3-dev libcap-dev libbsd-dev libnet1-dev libgnutls28-dev uuid-dev \
      python3 python3-protobuf libnftables-dev \
    && dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\n' \
      > /opt/factorybox-native-packages.tsv \
    && cp /etc/apt/sources.list /opt/factorybox-native-sources.list \
    && rm -rf /var/lib/apt/lists/*
