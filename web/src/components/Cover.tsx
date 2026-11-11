import { useEffect, useRef, useState } from 'react'
import { coverUrl, type Book } from '../api'
import { pdfThumbnail } from '../pdf'

const PALETTE: Array<[string, string]> = [
  ['#dfe8e1', '#3f5a4c'], // sage
  ['#f4e1dd', '#7a4842'], // rose
  ['#e4e1f2', '#4e4878'], // lavender
  ['#dde9f0', '#3c5a6d'], // sky
  ['#f3ead0', '#6b5a2c'], // butter
  ['#f6e2d4', '#7c4d31'], // peach
]

function hash(s: string) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

function Placeholder({ book }: { book: Book }) {
  const [bg, fg] = PALETTE[hash(book.title) % PALETTE.length]
  return (
    <div className="cover-placeholder" style={{ background: bg, color: fg }}>
      <span className="cover-placeholder-rule" style={{ background: fg }} />
      <span className="cover-placeholder-title">{book.title}</span>
      {book.author && <span className="cover-placeholder-author">{book.author}</span>}
    </div>
  )
}

export function Cover({ book }: { book: Book }) {
  const epubId = book.files.find((f) => f.format === 'epub' && f.hasCover)?.id
  const pdfId = book.files.find((f) => f.format === 'pdf')?.id
  const [src, setSrc] = useState<string | null>(epubId ? coverUrl(epubId) : null)
  const [failed, setFailed] = useState(false)
  const ref = useRef<HTMLDivElement>(null)

  // PDFs have no embedded cover; render page 1 once the card scrolls into view.
  useEffect(() => {
    if (epubId || !pdfId || !ref.current) return
    let alive = true
    const io = new IntersectionObserver(
      (entries) => {
        if (!entries.some((e) => e.isIntersecting)) return
        io.disconnect()
        pdfThumbnail(pdfId).then(
          (url) => alive && setSrc(url),
          () => alive && setFailed(true),
        )
      },
      { rootMargin: '200px' },
    )
    io.observe(ref.current)
    return () => {
      alive = false
      io.disconnect()
    }
  }, [epubId, pdfId])

  return (
    <div className="cover" ref={ref}>
      {src && !failed ? (
        <img src={src} alt="" loading="lazy" onError={() => setFailed(true)} />
      ) : (
        <Placeholder book={book} />
      )}
    </div>
  )
}
