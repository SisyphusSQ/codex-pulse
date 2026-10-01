FROM golang:1.27.1-alpine3.24 AS builder

WORKDIR /src
# 从 monorepo 根构建，保留 server 的 replace .. 模块关系。
COPY go.mod go.sum ./
COPY api ./api
COPY internal ./internal
COPY server ./server
WORKDIR /src/server
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/codex-pulse-server ./app/main.go

FROM alpine:3.24.1
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S app \
    && adduser -S -G app app \
    && mkdir -p /app/config /app/logs \
    && chown -R app:app /app

WORKDIR /app
COPY --from=builder /out/codex-pulse-server /app/codex-pulse-server
COPY --from=builder /src/server/config/config_docker.yml /app/config/config.yml
# 构建前在 server 执行 make web-build；缺少完整产物时明确失败。
COPY --from=builder /src/server/web/dist /app/web

USER app
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD wget -qO- http://127.0.0.1:8080/health >/dev/null || exit 1
ENTRYPOINT ["/app/codex-pulse-server"]
CMD ["http", "-c", "/app/config/config.yml"]
