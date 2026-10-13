FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ .
RUN npm run build

FROM golang:1.22-alpine AS gateway
WORKDIR /src
COPY web/gateway/go.mod ./
COPY web/gateway/main.go ./
RUN go mod tidy
RUN CGO_ENABLED=0 go build -o /gateway .

FROM alpine:3.20
COPY --from=gateway /gateway /gateway
COPY --from=web /src/dist /app/dist
EXPOSE 5190
ENTRYPOINT ["/gateway"]
