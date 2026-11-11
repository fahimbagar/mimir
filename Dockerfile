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
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mimir ./cmd/mimir

# Static binary + built assets; runs as uid 65532 with no shell.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/mimir /app/mimir
COPY --from=web /src/web/dist /app/web
EXPOSE 8080
ENTRYPOINT ["/app/mimir", "-addr", ":8080", "-books", "/books", "-static", "/app/web"]
