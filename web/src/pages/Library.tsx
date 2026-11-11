import { useEffect, useMemo, useState } from 'react'
import { findFile, formatSize, loadBooks, type Book, type Format } from '../api'
import { Cover } from '../components/Cover'
import { navigate } from '../router'
import { getRecent } from '../storage'

type Filter = 'all' | Format

function openBook(book: Book) {
  // Reopen whichever format was read last, else the first one (EPUB sorts first).
  const recent = getRecent().find((r) => book.files.some((f) => f.id === r.fileId))
  navigate(`/read/${recent?.fileId ?? book.files[0].id}`)
}

function BookCard({ book, progress }: { book: Book; progress?: number }) {
  return (
    <article className="card">
      <button className="card-cover" onClick={() => openBook(book)} aria-label={`Open ${book.title}`}>
        <Cover book={book} />
        {progress != null && (
          <span className="card-progress" aria-hidden>
            <span style={{ width: `${Math.round(progress * 100)}%` }} />
          </span>
        )}
      </button>
      <div className="card-body">
        <h3 className="card-title" title={book.title}>
          {book.title}
        </h3>
        {book.author && <p className="card-author">{book.author}</p>}
        <div className="card-formats">
          {book.files.map((f) => (
            <button
              key={f.id}
              className={`pill pill-${f.format}`}
              onClick={() => navigate(`/read/${f.id}`)}
              title={`${f.name} · ${formatSize(f.size)}`}
            >
              {f.format.toUpperCase()}
            </button>
          ))}
        </div>
      </div>
    </article>
  )
}

export function Library() {
  const [books, setBooks] = useState<Book[] | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [query, setQuery] = useState('')
  const [filter, setFilter] = useState<Filter>('all')

  useEffect(() => {
    document.title = 'Mimir Library'
    loadBooks(true).then(setBooks, (e: Error) => setError(e.message))
  }, [])

  const shown = useMemo(() => {
    if (!books) return []
    const q = query.trim().toLowerCase()
    return books.filter(
      (b) =>
        (filter === 'all' || b.files.some((f) => f.format === filter)) &&
        (!q || b.title.toLowerCase().includes(q) || b.author?.toLowerCase().includes(q)),
    )
  }, [books, query, filter])

  const recent = useMemo(() => {
    if (!books) return []
    const seen = new Set<string>()
    const out: Array<{ book: Book; progress: number }> = []
    for (const r of getRecent()) {
      const hit = findFile(books, r.fileId)
      if (hit && !seen.has(hit.book.id)) {
        seen.add(hit.book.id)
        out.push({ book: hit.book, progress: r.progress })
      }
    }
    return out.slice(0, 6)
  }, [books])

  const searching = query.trim() !== '' || filter !== 'all'

  return (
    <div className="library">
      <header className="library-header">
        <div className="brand">
          <span className="brand-mark" aria-hidden>
            <svg viewBox="0 0 32 32">
              <path d="M8 9.5c3-1 6-.6 8 1.2v12.6c-2-1.8-5-2.2-8-1.2zM24 9.5c-3-1-6-.6-8 1.2v12.6c2-1.8 5-2.2 8-1.2z" />
            </svg>
          </span>
          <div>
            <h1>Mimir</h1>
            <p>{books ? `${books.length} ${books.length === 1 ? 'book' : 'books'} on the shelf` : 'Your private library'}</p>
          </div>
        </div>
        <div className="toolbar">
          <label className="search">
            <svg viewBox="0 0 24 24" aria-hidden>
              <circle cx="11" cy="11" r="6.5" />
              <path d="m20 20-4.2-4.2" />
            </svg>
            <input
              type="search"
              placeholder="Search title or author"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </label>
          <div className="segmented" role="radiogroup" aria-label="Format">
            {(['all', 'epub', 'pdf'] as Filter[]).map((f) => (
              <button key={f} role="radio" aria-checked={filter === f} onClick={() => setFilter(f)}>
                {f === 'all' ? 'All' : f.toUpperCase()}
              </button>
            ))}
          </div>
        </div>
      </header>

      {error && <div className="notice notice-error">{error}</div>}
      {!books && !error && <div className="notice">Opening the shelves…</div>}

      {books && !searching && recent.length > 0 && (
        <section className="shelf">
          <h2>Continue reading</h2>
          <div className="grid">
            {recent.map(({ book, progress }) => (
              <BookCard key={book.id} book={book} progress={progress} />
            ))}
          </div>
        </section>
      )}

      {books && (
        <section className="shelf">
          <h2>{searching ? `${shown.length} found` : 'All books'}</h2>
          {shown.length === 0 ? (
            <div className="notice">
              {books.length === 0 ? 'No PDF or EPUB files in the ebook folder yet.' : 'Nothing matches that search.'}
            </div>
          ) : (
            <div className="grid">
              {shown.map((b) => (
                <BookCard key={b.id} book={b} />
              ))}
            </div>
          )}
        </section>
      )}
    </div>
  )
}
