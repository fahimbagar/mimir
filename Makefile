.PHONY: build web run dev-api dev-web test lint

build: web
	go build -o bin/mimir ./cmd/mimir

web:
	cd web && npm install && npm run build

run: build
	./bin/mimir

# Development: run both, then open http://localhost:5173/auth/dev-login?email=$(DEV_EMAIL)
# (Vite proxies /api and /auth). The dev build signs in without Google.
DEV_EMAIL ?= dev@example.com

dev-api:
	MIMIR_BASE_URL=http://localhost:5173 GOOGLE_CLIENT_ID=dev GOOGLE_CLIENT_SECRET=dev \
	MIMIR_ALLOWED_EMAILS=$(DEV_EMAIL) go run -tags dev ./cmd/mimir -db ./data/dev.db

dev-web:
	cd web && npm run dev

test:
	go test ./...
	cd web && npm run typecheck && npm test

lint:
	golangci-lint run ./...
	cd web && npm run lint
