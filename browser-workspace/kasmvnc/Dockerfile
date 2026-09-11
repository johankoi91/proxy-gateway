FROM docker.m.daocloud.io/library/debian:bookworm-slim@sha256:88200866dfff7ea7f5cbcb6ec7c8a701889efe6fe859fe64d6990e4b07ea4171

ARG KASMVNC_VERSION=1.5.0
ARG KASMVNC_DEB_SHA256=aa83a1a6c9069d1a02239988b07a3a2a082a433042b4d4ee2b9e9f6b2df9643c

COPY vendor-kasmvnc.deb /tmp/kasmvnc.deb

RUN sed -i \
      -e 's|http://deb.debian.org/debian|http://mirrors.aliyun.com/debian|g' \
      -e 's|http://deb.debian.org/debian-security|http://mirrors.aliyun.com/debian-security|g' \
      /etc/apt/sources.list.d/debian.sources \
    && apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates chromium dbus-x11 expect fonts-noto-cjk openbox \
      procps ssl-cert x11-xserver-utils xauth \
    && echo "${KASMVNC_DEB_SHA256}  /tmp/kasmvnc.deb" | sha256sum -c - \
    && apt-get install -y --no-install-recommends /tmp/kasmvnc.deb \
    && rm -f /tmp/kasmvnc.deb \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 --shell /bin/bash browser \
    && usermod -a -G ssl-cert browser

RUN apt-get update \
    && apt-get install -y --no-install-recommends chromium-sandbox \
    && rm -rf /var/lib/apt/lists/*

COPY entrypoint.sh /usr/local/bin/kasm-browser-entrypoint
COPY kasmvnc.yaml xstartup openbox-autostart /opt/kasm-browser/

RUN chmod 0755 /usr/local/bin/kasm-browser-entrypoint \
    && chmod 0644 /opt/kasm-browser/kasmvnc.yaml \
    && chmod 0755 /opt/kasm-browser/xstartup /opt/kasm-browser/openbox-autostart \
    && chown -R browser:browser /home/browser /opt/kasm-browser

USER browser
ENV HOME=/home/browser \
    LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    TZ=Asia/Tokyo \
    KASM_USER=admin

VOLUME ["/home/browser"]
EXPOSE 8443/tcp 8443/udp

ENTRYPOINT ["/usr/local/bin/kasm-browser-entrypoint"]
