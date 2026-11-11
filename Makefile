.PHONY: build web run dev-api dev-web test lint

build: web
	go build -o bin/mimir ./cmd/mimir

web:
	cd web && npm install && npm run build

run: build
	./bin/mimir

# Development: run both, then open http://localhost:5173 (Vite proxies /api).
dev-api:
	go run ./cmd/mimir

dev-web:
	cd web && npm run dev

test:
	go test ./...
	cd web && npm run typecheck && npm test

lint:
	golangci-lint run ./...
	cd web && npm run lint
