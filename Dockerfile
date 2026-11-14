# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# No build tags: the dev-only login is not compiled into the image.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mimir ./cmd/mimir \
 && mkdir -p /out/data

# Static binary + built assets; runs as uid 65532 with no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/mimir /app/mimir
COPY --from=web /src/web/dist /app/web
# Owned by the runtime user so a fresh named volume mounted here is writable.
COPY --from=server --chown=65532:65532 /out/data /data
EXPOSE 8080
ENTRYPOINT ["/app/mimir", "-addr", ":8080", "-books", "/books", "-static", "/app/web", "-db", "/data/mimir.db"]
