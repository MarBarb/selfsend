FROM node:24-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
ARG VERSION=dev
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/selfsend ./cmd/selfsend

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S selfsend && adduser -S -G selfsend selfsend
COPY --from=build /out/selfsend /usr/local/bin/selfsend
RUN mkdir /data && chown selfsend:selfsend /data
USER selfsend
VOLUME ["/data"]
EXPOSE 8080
ENV SELFSEND_DATA_DIR=/data SELFSEND_LISTEN=:8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/api/health || exit 1
ENTRYPOINT ["/usr/local/bin/selfsend"]
