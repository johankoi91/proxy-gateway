FROM docker.m.daocloud.io/library/debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

COPY vendor/linux-arm64-filebrowser-quantum /tmp/filebrowser
RUN echo '0ad12910ffe51bc3ad06096af9559147f1126d074ed5c8a1afdf81ae5d7ebf24  /tmp/filebrowser' | sha256sum -c - \
    && install -m 0755 /tmp/filebrowser /usr/local/bin/filebrowser \
    && useradd --create-home --uid 1000 --shell /usr/sbin/nologin fileportal \
    && install -d -o fileportal -g fileportal -m 0750 /database \
    && rm /tmp/filebrowser

COPY config.yaml /etc/filebrowser/config.yaml
COPY entrypoint.sh /usr/local/bin/file-portal-entrypoint
RUN chmod 0755 /usr/local/bin/file-portal-entrypoint

USER fileportal
ENTRYPOINT ["/usr/local/bin/file-portal-entrypoint"]
