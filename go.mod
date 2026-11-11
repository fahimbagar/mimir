module mimir

go 1.27.3

require github.com/go-chi/chi/v5 v5.5.0

// node_modules ships stray Go packages; keep go test/vet ./... out of it.
ignore ./web
