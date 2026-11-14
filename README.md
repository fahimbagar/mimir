# Mimir

A private ebook library for your own PDF and EPUB files. Readers sign in with an invited Google account. A Go (chi) server serves the books, a small JSON API and a SQLite database of reading progress. The React + TypeScript frontend renders PDFs with pdf.js and EPUBs with epub.js.

## Features

- Sign-in with Google, limited to the email addresses you list
- Library shelf with covers, search by title or author, and an All / EPUB / PDF filter
- "Continue reading" and reading positions stored per account, so they follow you between devices
- One-page or two-page layout. Two-page PDFs follow a printed book: the cover alone, then 2–3, 4–5.
- Full screen mode that hides the reader's bars and uses the whole screen, also on phones
- Bookmarks panel built from the PDF outline or the EPUB table of contents, shown only when the book has one
- Page numbers you can show or hide, plus go-to-page for both formats

## Requirements

- Go 1.27+
- Node.js 22+ (Node 20.19 builds, with engine warnings from npm and pdf.js)
- A Google OAuth client (see below)

## Deploying with Docker

Mimir runs behind your own reverse proxy, which handles HTTPS for `example.dev`.

1. Create the Google OAuth client: Google Cloud Console > APIs & Services > Credentials > Create credentials > OAuth client ID > Web application. Add this authorized redirect URI:
   ```
   https://example.dev/auth/callback
   ```
   If the consent screen is in "Testing" mode, add the same accounts as test users there.
2. Configure and start:
   ```sh
   cp .env.example .env    # fill in GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET, MIMIR_ALLOWED_EMAILS
   docker compose up -d --build
   ```
3. Point the proxy at `127.0.0.1:8080`. For Caddy:
   ```
   example.dev {
       reverse_proxy 127.0.0.1:8080
   }
   ```
   For nginx, inside the `server` block for the domain:
   ```nginx
   location / {
       proxy_pass http://127.0.0.1:8080;
       proxy_set_header Host $host;
   }
   ```
   No other headers are needed. The app takes its public address from `MIMIR_BASE_URL`, not from forwarded headers.

Compose refuses to start if a required variable is missing. The image is a static binary on distroless and runs as a non-root user with a read-only filesystem. Books are mounted read-only. The database lives in the `mimir-data` volume. To back it up, stop the app briefly so the SQLite write-ahead log is folded in, then copy the files:

```sh
docker compose stop mimir
docker run --rm -v mimir_mimir-data:/data -v "$PWD":/backup alpine sh -c 'cp /data/mimir.db* /backup/'
docker compose start mimir
```

## Configuration

Environment variables. The flags `-base-url`, `-addr`, `-books`, `-db` and `-static` override them.

| Variable               | Default                        | Meaning                                              |
| ---------------------- | ------------------------------ | ---------------------------------------------------- |
| `MIMIR_BASE_URL`       | `https://example.dev` | public origin; Google redirects to `/auth/callback`  |
| `GOOGLE_CLIENT_ID`     | (required)                     | OAuth client ID                                      |
| `GOOGLE_CLIENT_SECRET` | (required)                     | OAuth client secret                                  |
| `MIMIR_ALLOWED_EMAILS` | (required)                     | comma-separated Google accounts allowed to sign in   |
| `MIMIR_ADDR`           | `:8080`                        | listen address                                       |
| `MIMIR_BOOKS`          | `./ebook`                      | folder with `.pdf` / `.epub` files                   |
| `MIMIR_DB`             | `./data/mimir.db`              | SQLite database                                      |
| `MIMIR_STATIC`         | `./web/dist`                   | built frontend                                       |

In compose, `MIMIR_BIND`, `MIMIR_PORT` and `MIMIR_BOOKS_DIR` choose the published address and the host books folder.

Changing `MIMIR_ALLOWED_EMAILS` takes effect on restart. Someone removed from the list is locked out on their next request, even with a valid session cookie.

## Security

- Every API route, including the PDF/EPUB bytes and covers, needs a session for an allowlisted, Google-verified email. Without one the server answers 401.
- Sign-in uses the OpenID Connect code flow with PKCE, state and nonce. ID tokens are checked against Google's signing keys, issuer and client ID.
- Session tokens are 256-bit random values in an `HttpOnly`, `Secure`, `SameSite=Lax`, `__Host-` cookie. The database stores only their SHA-256, and sessions expire after 30 days.
- Cross-site POST/PUT requests are rejected (CSRF), and the JSON input is strictly validated.
- Responses carry a strict Content-Security-Policy, no framing, `nosniff`, `no-referrer` and HSTS. Book files are sent with `no-store`, and EPUB covers are sandboxed because they may contain SVG scripts.
- Request logs record paths without query strings, so OAuth codes never reach the logs.
- The production image doesn't contain the dev-only login (see below).

A signed-in reader can still save what their browser receives; that is true of any web reader. Access control decides *who* can read, not what an invited reader does with a page.

## Local development

```sh
make dev-api    # dev build on :8080, signs in without Google
make dev-web    # Vite on :5173, proxies /api and /auth
```

Then open http://localhost:5173/auth/dev-login?email=dev@example.com. The dev login only exists in builds made with `-tags dev`.

`make test` runs the Go tests, the frontend tests and the type check. `make lint` runs golangci-lint and ESLint. CI (`.github/workflows/ci.yml`) runs both, scans dependencies with govulncheck and builds the Docker image.

## Adding books

Copy `.pdf` or `.epub` files into the books folder. Subfolders work. The folder is rescanned each time the library page loads, so there is no import step. Book files are ignored by git; only `ebook/.keep` is tracked.

Files with the same name and different extensions show up as one book with both formats. Titles, authors and covers come from EPUB metadata. PDFs use the file name, with tags like `(z-library.sk, …)` stripped and `Title (Author)` split, and the cover is page 1 rendered in the browser.

## Page numbers in EPUBs

EPUBs reflow to fit the screen, so they have no fixed pages. Mimir counts one page per ~1600 characters of text, the same unit epub.js uses for progress. The count is stable for a given book but won't match the printed edition. A bookmark shows the first page of the file it points into. The first time a long EPUB is opened, computing pages takes a few seconds; after that it is cached in the browser.

## API

All `/api` routes need a session.

| Endpoint                      | Does                                                          |
| ----------------------------- | ------------------------------------------------------------- |
| `GET /api/me`                 | the signed-in user                                            |
| `GET /api/books`              | books with their files, titles and authors                    |
| `GET /api/files/{id}`         | the file, with Range support for large PDFs                   |
| `GET /api/files/{id}/cover`   | embedded EPUB cover image, or 404                             |
| `GET /api/progress`           | the user's reading positions, most recent first               |
| `PUT /api/progress/{id}`      | save `{"position": <page or CFI>, "progress": 0..1}`          |
| `GET /auth/login`             | start Google sign-in                                          |
| `POST /auth/logout`           | end the session                                               |

File IDs come from the scan. The server never opens a path sent by the client.

## Reader keys

| Key                      | Action                                                        |
| ------------------------ | ------------------------------------------------------------- |
| `→`, `Space`, `PageDown` | next page                                                     |
| `←`, `PageUp`            | previous page                                                 |
| `F`                      | full screen                                                   |
| `Esc`                    | leave full screen, close bookmarks on small screens, or back  |

On phones and in full screen, tap the left or right edge of the page to turn it. Two-page mode is offered only when the reading area is at least 720px wide.

## License

MIT. See [LICENSE](LICENSE).
