# Mimir

A private ebook library for the PDF and EPUB files in `./ebook/`. A Go (chi) server serves the files and a small JSON API. The React + TypeScript frontend renders PDFs with pdf.js and EPUBs with epub.js.

## Features

- Library shelf with covers, search by title or author, and an All / EPUB / PDF filter
- "Continue reading" row with progress, reopening the format you read last
- One-page or two-page layout. Two-page PDFs follow a printed book: the cover alone, then 2–3, 4–5.
- Bookmarks panel built from the PDF outline or the EPUB table of contents, shown only when the book has one. Entries start expanded, can be collapsed, and follow your position.
- Page numbers you can show or hide, plus go-to-page for both formats
- Reading position, layout and settings saved per browser

## Requirements

- Go 1.27+
- Node.js 22+ (Node 20.19 builds, with engine warnings from npm and pdf.js)

## Run

```sh
make run            # builds the frontend and the binary, serves on :8080
```

Server flags:

| Flag      | Default      | Meaning                          |
| --------- | ------------ | -------------------------------- |
| `-addr`   | `:8080`      | listen address                   |
| `-books`  | `./ebook`    | folder with `.pdf` / `.epub`     |
| `-static` | `./web/dist` | built frontend                   |

For development, run `make dev-api` and `make dev-web` in two terminals, then open http://localhost:5173. Vite proxies `/api` to the Go server.

`make test` runs the Go tests, the frontend tests and the type check. `make lint` runs golangci-lint and ESLint. CI (`.github/workflows/ci.yml`) runs both and builds the Docker image.

## Docker

```sh
cp .env.example .env    # set MIMIR_BIND, MIMIR_PORT, MIMIR_BOOKS
docker compose up -d --build
```

The image is a static binary on distroless (about 13 MB) and runs as a non-root user with a read-only filesystem. Books are mounted read-only from `MIMIR_BOOKS`. With no `.env`, the port is published on `127.0.0.1` only.

## Adding books

Copy `.pdf` or `.epub` files into `ebook/`, or the folder set in `MIMIR_BOOKS`. Subfolders work. The folder is rescanned each time the library page loads, so there is no import step. Book files are ignored by git; only `ebook/.keep` is tracked.

Files with the same name and different extensions show up as one book with both formats. Titles, authors and covers come from EPUB metadata. PDFs use the file name, with tags like `(z-library.sk, …)` stripped and `Title (Author)` split, and the cover is page 1 rendered in the browser.

## Page numbers in EPUBs

EPUBs reflow to fit the screen, so they have no fixed pages. Mimir counts one page per ~1600 characters of text, the same unit epub.js uses for progress. The count is stable for a given book but won't match the printed edition. A bookmark shows the first page of the file it points into. The first time a long EPUB is opened, computing pages takes a few seconds; after that it is cached in the browser.

## API

| Endpoint                     | Returns                                              |
| ---------------------------- | ---------------------------------------------------- |
| `GET /api/books`             | books with their files, titles and authors           |
| `GET /api/files/{id}`        | the file, with Range support for large PDFs          |
| `GET /api/files/{id}/cover`  | embedded EPUB cover image, or 404                    |

File IDs come from the scan. The server never opens a path sent by the client.

There is no authentication. Run it on a trusted network only.

## Reader keys

| Key                           | Action                                         |
| ----------------------------- | ---------------------------------------------- |
| `→`, `Space`, `PageDown`      | next page                                      |
| `←`, `PageUp`                 | previous page                                  |
| `Esc`                         | close bookmarks on small screens, else library |

Two-page mode falls back to one page when the reading area is narrower than 720px.

## License

MIT. See [LICENSE](LICENSE).
