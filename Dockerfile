ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.27-alpine
ARG ALPINE_IMAGE=alpine:3.21

FROM ${NODE_IMAGE} AS web
ARG NPM_REGISTRY=
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN if [ -n "$NPM_REGISTRY" ]; then npm config set registry "$NPM_REGISTRY"; fi
RUN npm ci
COPY web/ ./
RUN npm run build

FROM ${GO_IMAGE} AS go
ENV GOPROXY=https://goproxy.cn,direct
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /build/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/oss-web ./cmd/oss-web

FROM ${ALPINE_IMAGE}
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 ossweb
WORKDIR /app
COPY --from=go /out/oss-web /app/oss-web
RUN mkdir -p /data && chown ossweb:ossweb /data
USER ossweb
EXPOSE 8080
VOLUME ["/data"]
ENTRYPOINT ["/app/oss-web"]
