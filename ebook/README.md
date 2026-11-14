# ebook

Put your `.pdf` and `.epub` files here. Mimir rescans this folder every time the library page loads, so there's no import step. Subfolders work.

- A PDF and an EPUB with the same file name show up as one book with both formats.
- Book files are ignored by git; only this README and `.keep` are tracked.
- With Docker the folder is mounted read-only. To use another folder, set `MIMIR_BOOKS_DIR` in `.env`.
