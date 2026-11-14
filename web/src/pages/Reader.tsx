import { useCallback, useEffect, useEffectEvent, useRef, useState } from 'react'
import { findFile, loadBooks, loadProgress, saveProgress, type Book, type BookFile } from '../api'
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

// iPhone Safari has no Fullscreen API for pages. Full screen mode still
// hides the reader's own bars there; only the browser's stay.
const canFullscreen = typeof document !== 'undefined' && document.fullscreenEnabled

// The PDF magnifier follows a mouse, so it's offered only where one is the
// main pointer.
const canMagnify = typeof window !== 'undefined' && window.matchMedia('(hover: hover) and (pointer: fine)').matches

// Below this reading-area width the margins around pages are trimmed.
const COMPACT_WIDTH = 600

export function Reader({ fileId }: { fileId: string }) {
  const [hit, setHit] = useState<{ book: Book; file: BookFile; start?: number | string } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [spread, setSpreadState] = useState<Spread>(getSpread)
  const surfaceRef = useRef<HTMLDivElement>(null)
  const [surfaceWidth, setSurfaceWidth] = useState(() => window.innerWidth)
  const [immersive, setImmersive] = useState(false)
  const [status, setStatus] = useState<ReaderStatus | null>(null)
  const controllerRef = useRef<ReaderController | null>(null)
  const [showNums, setShowNums] = useState(getPageNumbers)
  const [outline, setOutline] = useState<OutlineItem[]>([])
  const [tocOpen, setTocOpenState] = useState(getTocOpen)
  const [magnify, setMagnify] = useState(false)

  // Positions are saved to the server a moment after the reader stops on a
  // page, and right away when the tab is hidden or closed.
  const pending = useRef<{ position: number | string; progress: number } | null>(null)
  const lastProgress = useRef(0)
  const saveTimer = useRef(0)

  // App keys this component by fileId, so switching books starts fresh.
  useEffect(() => {
    Promise.all([loadBooks(), loadProgress()]).then(
      ([books, progress]) => {
        const found = findFile(books, fileId)
        if (!found) {
          setError('This book is no longer on the shelf.')
          return
        }
        const saved = progress.find((p) => p.fileId === fileId)
        lastProgress.current = saved?.progress ?? 0
        setHit({ ...found, start: saved?.position })
        document.title = `${found.book.title} · Mimir`
      },
      (e: Error) => setError(e.message),
    )
  }, [fileId])

  const flush = useCallback(
    (keepalive = false) => {
      window.clearTimeout(saveTimer.current)
      const p = pending.current
      if (!p) return
      pending.current = null
      saveProgress(fileId, p.position, p.progress, keepalive).catch((e: Error) => console.warn(e.message))
    },
    [fileId],
  )

  const onStatus = useCallback(
    (s: ReaderStatus) => {
      setStatus(s)
      // An EPUB reports progress only once its pages are counted; until then
      // keep the last known value instead of resetting it to 0.
      if (s.progress != null) lastProgress.current = s.progress
      pending.current = { position: s.position, progress: lastProgress.current }
      window.clearTimeout(saveTimer.current)
      saveTimer.current = window.setTimeout(() => flush(), 1500)
    },
    [flush],
  )

  useEffect(() => {
    const onHidden = () => {
      if (document.visibilityState === 'hidden') flush(true)
    }
    const onPageHide = () => flush(true)
    document.addEventListener('visibilitychange', onHidden)
    window.addEventListener('pagehide', onPageHide)
    return () => {
      document.removeEventListener('visibilitychange', onHidden)
      window.removeEventListener('pagehide', onPageHide)
      flush(true)
    }
  }, [flush])

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
    // Leaving browser full screen (Esc, Android back) also leaves the mode.
    const sync = () => {
      if (!document.fullscreenElement) setImmersive(false)
    }
    document.addEventListener('fullscreenchange', sync)
    return () => {
      document.removeEventListener('fullscreenchange', sync)
      // Leaving the reader shouldn't leave the library stuck in full screen.
      if (document.fullscreenElement) void document.exitFullscreen()
    }
  }, [])

  const toggleImmersive = () => {
    if (immersive) {
      setImmersive(false)
      if (document.fullscreenElement) void document.exitFullscreen()
      return
    }
    setImmersive(true)
    if (canFullscreen && !document.fullscreenElement) {
      document.documentElement.requestFullscreen().catch(() => {
        // Refused (e.g. no user gesture): stay in the mode without browser
        // full screen; the reader's bars are hidden either way.
      })
    }
  }

  const showMagnifier = canMagnify && hit?.file.format === 'pdf'

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
        toggleImmersive()
        break
      case 'm':
        if (!showMagnifier) return
        setMagnify((m) => !m)
        break
      case 'Escape':
        if (magnify) {
          setMagnify(false)
          break
        }
        if (immersive) {
          toggleImmersive()
          break
        }
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
    <div className={`reader ${immersive ? 'is-immersive' : ''}`}>
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
          {showMagnifier && (
            <button
              className="icon-btn"
              aria-pressed={magnify}
              title={magnify ? 'Hide magnifier (M)' : 'Magnifier (M)'}
              onClick={() => setMagnify(!magnify)}
            >
              <svg viewBox="0 0 24 24" aria-hidden>
                <path d="M17 11a6 6 0 1 1-12 0 6 6 0 0 1 12 0ZM15.5 15.5 20 20M11 8.5v5M8.5 11h5" />
              </svg>
            </button>
          )}
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
          <button className="icon-btn" title="Full screen (F)" onClick={toggleImmersive}>
            <svg viewBox="0 0 24 24" aria-hidden>
              <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />
            </svg>
          </button>
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
              initialPosition={hit.start}
              spread={shownSpread}
              controllerRef={controllerRef}
              onStatus={onStatus}
              onOutline={setOutline}
              showPageNumbers={showNums}
              compact={immersive || surfaceWidth < COMPACT_WIDTH}
              magnify={magnify}
            />
          )}
          {hit?.file.format === 'epub' && (
            <EpubReader
              key={fileId}
              fileId={fileId}
              initialPosition={hit.start}
              spread={shownSpread}
              controllerRef={controllerRef}
              onStatus={onStatus}
              onOutline={setOutline}
              showPageNumbers={showNums}
              compact={immersive || surfaceWidth < COMPACT_WIDTH}
            />
          )}
        </div>
        <button className="turn turn-next" aria-label="Next page" onClick={() => controllerRef.current?.next()}>
          <svg viewBox="0 0 24 24" aria-hidden>
            <path d="M9 5l7 7-7 7" />
          </svg>
        </button>
      </main>

      {immersive && (
        <div className="immersive-hud">
          <span className="immersive-status">{status?.label}</span>
          <button className="icon-btn immersive-exit" title="Exit full screen (F)" onClick={toggleImmersive}>
            <svg viewBox="0 0 24 24" aria-hidden>
              <path d="M9 4v5H4M15 4v5h5M9 20v-5H4M15 20v-5h5" />
            </svg>
          </button>
          <div className="immersive-progress" aria-hidden>
            <span style={{ width: `${Math.round((status?.progress ?? 0) * 100)}%` }} />
          </div>
        </div>
      )}

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
