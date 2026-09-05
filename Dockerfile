# syntax=docker/dockerfile:1.7
FROM node:20-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.24-alpine AS go-build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-build /src/internal/httpapi/static/ ./internal/httpapi/static/
ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/vpn-gateway ./cmd/gateway

FROM alpine:3.21
ARG XRAY_VERSION=26.3.27
ARG XRAY_SHA256=23cd9af937744d97776ee35ecad4972cf4b2109d1e0fe6be9930467608f7c8ae
RUN apk add --no-cache \
      bash ca-certificates curl dnsmasq haproxy iproute2 iptables supervisor \
      tzdata unzip wireguard-tools \
    && curl -fsSL --proto '=https' --tlsv1.2 \
      "https://github.com/XTLS/Xray-core/releases/download/v${XRAY_VERSION}/Xray-linux-64.zip" \
      -o /tmp/xray.zip \
    && echo "${XRAY_SHA256}  /tmp/xray.zip" | sha256sum -c - \
    && unzip -q /tmp/xray.zip xray geoip.dat geosite.dat -d /tmp/xray \
    && install -m 0755 /tmp/xray/xray /usr/local/bin/xray \
    && install -d -m 0755 /usr/local/share/xray \
    && install -m 0644 /tmp/xray/geoip.dat /tmp/xray/geosite.dat /usr/local/share/xray/ \
    && rm -rf /tmp/xray /tmp/xray.zip \
    && install -d -m 0700 /data /run/vpn-gateway
COPY --from=go-build /out/vpn-gateway /usr/local/bin/vpn-gateway
COPY packaging/supervisord.conf /etc/supervisord.conf
COPY packaging/haproxy.cfg /etc/haproxy/haproxy.cfg
COPY scripts/container-entrypoint.sh /usr/local/bin/container-entrypoint
RUN chmod 0755 /usr/local/bin/vpn-gateway /usr/local/bin/container-entrypoint \
    && ADMIN_SERVER_NAME=localhost haproxy -c -f /etc/haproxy/haproxy.cfg
ENV STATE_PATH=/data/state.json \
    RUNTIME_DIR=/run/vpn-gateway \
    LISTEN_ADDR=127.0.0.1:9080 \
    ADMIN_USER=admin \
    WG_PORT=443 \
    VLESS_PORT=443 \
    VLESS_LISTEN_PORT=8443 \
    ADMIN_SERVER_NAME=localhost \
    REALITY_TARGET=www.cloudflare.com:443 \
    REALITY_SERVER_NAME=www.cloudflare.com \
    DEFAULT_CLIENTS=mac,iphone,android
EXPOSE 443/tcp 443/udp 9080/tcp
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/container-entrypoint"]
