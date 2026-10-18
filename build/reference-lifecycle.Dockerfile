ARG BASE_IMAGE
FROM ${BASE_IMAGE}
COPY lab /lab
COPY ramctl /ramctl
COPY keeper /pipe-keeper
COPY criu /usr/local/sbin/criu
COPY sysbox-runc /usr/local/sbin/sysbox-runc-experiment
COPY sysbox-fs /usr/local/bin/sysbox-fs
COPY sysbox-mgr /usr/local/bin/sysbox-mgr
COPY theia-client.cjs /opt/theia-client.cjs
