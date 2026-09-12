FROM docker.m.daocloud.io/library/debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

RUN install -d -m 0755 /etc/ssl/certs /usr/local/libexec/lightnode-linux-controller

COPY dist/linux-arm64/lightnode-linux-controller /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller
COPY vendor/linux-arm64/sing-box /usr/local/libexec/lightnode-linux-controller/sing-box
COPY vendor/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN echo '4393306b90bb05502fce3b8f1754280f531c0f3ff47df9b1997b7831e3e543d0  /usr/local/libexec/lightnode-linux-controller/sing-box' | sha256sum -c - \
    && echo '9481fcd95f41b221f02f14d896535fe500bec539bc563c4cdca1acee483a8bdd  /etc/ssl/certs/ca-certificates.crt' | sha256sum -c - \
    && chmod 0755 \
      /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller \
      /usr/local/libexec/lightnode-linux-controller/sing-box

ENTRYPOINT ["/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller"]
CMD ["container-run"]
