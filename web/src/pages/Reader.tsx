import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react'
import { findFile, loadBooks, type Book, type BookFile } from '../api'
import { OutlinePanel } from '../components/OutlinePanel'
import { EpubReader } from '../readers/EpubReader'
import { PdfReader } from '../readers/PdfReader'
import { MIN_SPREAD_WIDTH, type OutlineItem, type ReaderController, type ReaderStatus } from '../readers/types'
import { navigate } from '../router'
import {
  getPageNumbers,
  getSpread,
  getTocOpen,
  setPageNumbers,
  setSpread as saveSpread,
  setTocOpen,
  touchRecent,
  type Spread,
} from '../storage'

function SpreadIcon({ pages }: { pages: Spread }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden>
      {pages === 1 ? (
        <rect x="7" y="4" width="10" height="16" rx="1.5" />
      ) : (
        <>
          <rect x="3" y="5" width="8.5" height="14" rx="1.5" />
          <rect x="12.5" y="5" width="8.5" height="14" rx="1.5" />
        </>
      )}
    </svg>
  )
}

function PageJump({ status, goTo }: { status: ReaderStatus; goTo: (n: number) => void }) {
  const [draft, setDraft] = useState<string | null>(null)
  if (status.page == null || status.pages == null) return null
  return (
    <form
      className="page-jump"
      onSubmit={(e) => {
        e.preventDefault()
        const n = Number(draft)
        if (Number.isFinite(n) && n >= 1) goTo(n)
        setDraft(null)
        ;(document.activeElement as HTMLElement | null)?.blur()
      }}
    >
      <input
        aria-label="Go to page"
        inputMode="numeric"
        value={draft ?? String(status.page)}
        onFocus={(e) => {
          setDraft(String(status.page))
          e.target.select()
        }}
        onChange={(e) => setDraft(e.target.value.replace(/\D/g, ''))}
        onBlur={() => setDraft(null)}
      />
      <span>/ {status.pages}</span>
    </form>
  )
}

// iPhone Safari has no Fullscreen API for pages, so the button is hidden there.
const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled

function toggleFullscreen() {
  if (document.fullscreenElement) void document.exitFullscreen()
  else
    document.documentElement.requestFullscreen().catch(() => {
      // Rejected when the browser refuses (e.g. no user gesture); the button
      // state follows fullscreenchange, so there is nothing to undo.
    })
}

export function Reader({ fileId }: { fileId: string }) {
  const [hit, setHit] = useState<{ book: Book; file: BookFile } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [spread, setSpreadState] = useState<Spread>(getSpread)
  const surfaceRef = useRef<HTMLDivElement>(null)
  const [surfaceWidth, setSurfaceWidth] = useState(() => window.innerWidth)
  const [fullscreen, setFullscreen] = useState(() => document.fullscreenElement != null)
  const [status, setStatus] = useState<ReaderStatus | null>(null)
  const controllerRef = useRef<ReaderController | null>(null)
  const [showNums, setShowNums] = useState(getPageNumbers)
  const [outline, setOutline] = useState<OutlineItem[]>([])
  const [tocOpen, setTocOpenState] = useState(getTocOpen)

  // App keys this component by fileId, so switching books starts fresh.
  useEffect(() => {
    loadBooks().then(
      (books) => {
        const found = findFile(books, fileId)
        if (found) {
          setHit(found)
          document.title = `${found.book.title} · Mimir`
        } else setError('This book is no longer on the shelf.')
      },
      (e: Error) => setError(e.message),
    )
  }, [fileId])

  const onStatus = useCallback(
    (s: ReaderStatus) => {
      setStatus(s)
      touchRecent(fileId, s.progress)
    },
    [fileId],
  )

  const showToc = (open: boolean) => {
    setTocOpenState(open)
    setTocOpen(open)
  }

  const pickBookmark = (item: OutlineItem) => {
    controllerRef.current?.goTo(item)
    // On narrow screens the panel covers the page, so get out of the way.
    if (window.matchMedia('(max-width: 900px)').matches) showToc(false)
  }

  // Two pages only fit in a wide reading area: phones in portrait, or a
  // narrow window with bookmarks open, get one page and no layout option.
  // Rotating a phone to landscape brings the saved two-page choice back.
  useEffect(() => {
    const el = surfaceRef.current
    if (!el) return
    const ro = new ResizeObserver(([entry]) => setSurfaceWidth(entry.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  const canSpread = surfaceWidth >= MIN_SPREAD_WIDTH
  const shownSpread: Spread = canSpread ? spread : 1

  useEffect(() => {
    const sync = () => setFullscreen(document.fullscreenElement != null)
    document.addEventListener('fullscreenchange', sync)
    return () => {
      document.removeEventListener('fullscreenchange', sync)
      // Leaving the reader shouldn't leave the library stuck in fullscreen.
      if (document.fullscreenElement) void document.exitFullscreen()
    }
  }, [])

  const togglePageNumbers = () => {
    setShowNums(!showNums)
    setPageNumbers(!showNums)
  }

  const chooseSpread = (s: Spread) => {
    setSpreadState(s)
    saveSpread(s)
  }

  const onKey = useEffectEvent((e: KeyboardEvent) => {
    const t = e.target as HTMLElement | null
    if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA')) return
    // Leave browser shortcuts like Ctrl+F alone.
    if (e.ctrlKey || e.metaKey || e.altKey) return
    const c = controllerRef.current
    switch (e.key) {
      case 'ArrowRight':
      case 'PageDown':
      case ' ':
        c?.next()
        break
      case 'ArrowLeft':
      case 'PageUp':
        c?.prev()
        break
      case 'f':
        if (canFullscreen) toggleFullscreen()
        break
      case 'Escape':
        if (tocOpen && window.matchMedia('(max-width: 900px)').matches) {
          setTocOpenState(false)
          break
        }
        navigate('/')
        return
      default:
        return
    }
    e.preventDefault()
  })

  useEffect(() => {
    const handler = (e: KeyboardEvent) => onKey(e)
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  return (
    <div className="reader">
      <header className="reader-bar">
        <div className="reader-lead">
          <button className="ghost-btn" onClick={() => navigate('/')}>
            <svg viewBox="0 0 24 24" aria-hidden>
              <path d="M15 5l-7 7 7 7" />
            </svg>
            <span>Library</span>
          </button>
          {outline.length > 0 && (
            <button
              className="icon-btn"
              aria-pressed={tocOpen}
              title={tocOpen ? 'Hide bookmarks' : 'Show bookmarks'}
              onClick={() => showToc(!tocOpen)}
            >
              <svg viewBox="0 0 24 24" aria-hidden>
                <path d="M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01" />
              </svg>
            </button>
          )}
        </div>
        <div className="reader-title">
          <strong>{hit?.book.title ?? ' '}</strong>
          {hit?.book.author && <span>{hit.book.author}</span>}
        </div>
        <div className="reader-actions">
          {hit && <span className={`pill pill-${hit.file.format} pill-static`}>{hit.file.format.toUpperCase()}</span>}
          <button
            className="icon-btn"
            aria-pressed={showNums}
            title={showNums ? 'Hide page numbers' : 'Show page numbers'}
            onClick={togglePageNumbers}
          >
            <svg viewBox="0 0 24 24" aria-hidden>
              <path d="M10 4 8 20M16 4l-2 16M5 9h15M4 15h15" />
            </svg>
          </button>
          {canSpread && (
            <div className="segmented segmented-icons" role="radiogroup" aria-label="Page layout">
              {([1, 2] as Spread[]).map((s) => (
                <button
                  key={s}
                  role="radio"
                  aria-checked={spread === s}
                  title={s === 1 ? 'Single page' : 'Two pages'}
                  onClick={() => chooseSpread(s)}
                >
                  <SpreadIcon pages={s} />
                </button>
              ))}
            </div>
          )}
          {canFullscreen && (
            <button
              className="icon-btn"
              aria-pressed={fullscreen}
              title={fullscreen ? 'Exit full screen (F)' : 'Full screen (F)'}
              onClick={toggleFullscreen}
            >
              <svg viewBox="0 0 24 24" aria-hidden>
                {fullscreen ? (
                  <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />
                ) : (
                  <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
                )}
              </svg>
            </button>
          )}
        </div>
      </header>

      <main className="reader-stage">
        {tocOpen && outline.length > 0 && (
          <>
            <div className="toc-backdrop" onClick={() => showToc(false)} />
            <OutlinePanel
              items={outline}
              activeId={status?.outlineId}
              showPages={showNums}
              onPick={pickBookmark}
              onClose={() => showToc(false)}
            />
          </>
        )}
        <button className="turn turn-prev" aria-label="Previous page" onClick={() => controllerRef.current?.prev()}>
          <svg viewBox="0 0 24 24" aria-hidden>
            <path d="M15 5l-7 7 7 7" />
          </svg>
        </button>
        <div className="reader-surface" ref={surfaceRef}>
          {error && <div className="notice notice-error">{error}</div>}
          {hit?.file.format === 'pdf' && (
            <PdfReader
              key={fileId}
              fileId={fileId}
              spread={shownSpread}
              controllerRef={controllerRef}
              onStatus={onStatus}
              onOutline={setOutline}
              showPageNumbers={showNums}
            />
          )}
          {hit?.file.format === 'epub' && (
            <EpubReader
              key={fileId}
              fileId={fileId}
              spread={shownSpread}
              controllerRef={controllerRef}
              onStatus={onStatus}
              onOutline={setOutline}
              showPageNumbers={showNums}
            />
          )}
        </div>
        <button className="turn turn-next" aria-label="Next page" onClick={() => controllerRef.current?.next()}>
          <svg viewBox="0 0 24 24" aria-hidden>
            <path d="M9 5l7 7-7 7" />
          </svg>
        </button>
      </main>

      <footer className="reader-foot">
        <div className="progress" aria-hidden>
          <span style={{ width: `${Math.round((status?.progress ?? 0) * 100)}%` }} />
        </div>
        <div className="reader-foot-row">
          <span className="reader-status">{status?.label ?? ' '}</span>
          {status && <PageJump status={status} goTo={(n) => controllerRef.current?.goToPage(n)} />}
        </div>
      </footer>
    </div>
  )
}
