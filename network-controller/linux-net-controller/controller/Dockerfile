FROM docker.m.daocloud.io/library/debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

RUN install -d -m 0755 /etc/ssl/certs /usr/local/libexec/lightnode-linux-controller

COPY dist/linux-amd64/lightnode-linux-controller /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller
COPY vendor/linux-amd64/sing-box /usr/local/libexec/lightnode-linux-controller/sing-box
COPY vendor/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN echo '57b3da14e264b6e05e8f46aee027c02d7dd7f1594d19aa39e2f4d2b9459bbd04  /usr/local/libexec/lightnode-linux-controller/sing-box' | sha256sum -c - \
    && echo '9481fcd95f41b221f02f14d896535fe500bec539bc563c4cdca1acee483a8bdd  /etc/ssl/certs/ca-certificates.crt' | sha256sum -c - \
    && chmod 0755 \
      /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller \
      /usr/local/libexec/lightnode-linux-controller/sing-box

ENTRYPOINT ["/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller"]
CMD ["container-run"]
