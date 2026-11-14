import { useEffect, useEffectEvent, useRef, useState } from 'react'
import ePub, { EpubCFI, type Book, type Rendition } from 'epubjs'
import type { Location } from 'epubjs/types/rendition'
import { fileUrl } from '../api'
import { getLocations, setLocations } from '../storage'
import { flattenOutline, fromEpubToc, itemForHref, sectionStartPage, withPages } from './outline'
import type { OutlineItem, ReaderProps } from './types'

// The reader page already decided whether two pages fit, so tell epub.js to
// obey rather than apply its own width threshold.
const spreadMode = (s: 1 | 2) => (s === 2 ? 'always' : 'none')

// EPUBs reflow, so they have no fixed pages. We count one "page" per
// location, the ~1600-character chunks epub.js already computes for progress.
const CHARS_PER_PAGE = 1600

export function EpubReader({ fileId, initialPosition, spread, controllerRef, onStatus, onOutline, showPageNumbers }: ReaderProps) {
  const hostRef = useRef<HTMLDivElement>(null)
  const renditionRef = useRef<Rendition | null>(null)
  const emitStatus = useEffectEvent(onStatus)
  const emitOutline = useEffectEvent(onOutline)
  const initialSpread = useRef(spread)
  const startAt = useRef(typeof initialPosition === 'string' ? initialPosition : undefined)
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [shownPages, setShownPages] = useState<[number, number] | null>(null)

  useEffect(() => {
    const host = hostRef.current
    if (!host) return
    const abort = new AbortController()
    let book: Book | null = null
    let flat: OutlineItem[] = []

    const pageOfCfi = (cfi: string) =>
      // Typed as Location, but epub.js returns the location index.
      (book!.locations.locationFromCfi(cfi) as unknown as number) + 1

    const report = (loc: Location) => {
      if (!book) return
      const locs = book.locations
      const ready = locs.length() > 0
      const pct = ready ? locs.percentageFromCfi(loc.start.cfi) : null
      const first = ready ? pageOfCfi(loc.start.cfi) : undefined
      const last = ready ? Math.max(first!, pageOfCfi(loc.end.cfi)) : undefined
      setShownPages(first != null ? [first, last!] : null)
      const item = itemForHref(flat, loc.start.href)
      const where =
        pct != null
          ? `${Math.round(pct * 100)}%`
          : `Page ${loc.start.displayed.page} of ${loc.start.displayed.total} in chapter`
      emitStatus({
        label: item ? `${item.label} · ${where}` : where,
        position: loc.start.cfi,
        progress: loc.atEnd ? 1 : pct,
        page: first,
        pages: ready ? locs.length() : undefined,
        outlineId: item?.id,
      })
    }

    ;(async () => {
      // epub.js guesses the format from the URL extension, which our API URLs
      // lack, so hand it the bytes instead.
      const res = await fetch(fileUrl(fileId), { signal: abort.signal })
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const data = await res.arrayBuffer()
      if (abort.signal.aborted) return

      book = ePub(data)
      const rendition = book.renderTo(host, {
        width: '100%',
        height: '100%',
        flow: 'paginated',
        spread: spreadMode(initialSpread.current),
        minSpreadWidth: 0,
        allowScriptedContent: false,
      })
      renditionRef.current = rendition
      rendition.themes.default({
        body: {
          color: '#37332d',
          'line-height': '1.65',
          'font-family': "'Iowan Old Style', 'Palatino Linotype', Palatino, Georgia, serif",
        },
        'a, a:visited': { color: '#5f8172' },
        '::selection': { background: '#dfe8e1' },
        img: { 'max-width': '100%' },
      })
      rendition.on('relocated', (loc: Location) => report(loc))
      // Key presses inside the book's iframe never reach our window listener.
      rendition.on('keydown', (e: KeyboardEvent) => {
        window.dispatchEvent(new KeyboardEvent('keydown', { key: e.key }))
      })

      const nav = await book.loaded.navigation
      const outline = fromEpubToc(nav.toc)
      flat = flattenOutline(outline)
      emitOutline(outline)

      await rendition.display(startAt.current)
      if (abort.signal.aborted) return
      setLoading(false)

      controllerRef.current = {
        next: () => void rendition.next(),
        prev: () => void rendition.prev(),
        goTo: (item) => item.href && void rendition.display(item.href),
        goToPage: (n) => {
          const total = book?.locations.length() ?? 0
          if (!total) return
          const idx = Math.min(total, Math.max(1, Math.round(n))) - 1
          void rendition.display(book!.locations.cfiFromLocation(idx))
        },
      }

      // Percentages need a full pass over the text. Cache it: it can take a
      // few seconds on long books.
      const cached = getLocations(fileId)
      if (cached) book.locations.load(cached)
      else {
        await book.locations.generate(CHARS_PER_PAGE)
        if (abort.signal.aborted) return
        setLocations(fileId, book.locations.save())
      }

      // A bookmark's page is the first page of its spine section. Entries
      // that point into the middle of a file get that file's first page.
      const locs = book.locations
      const spineOfPage = Array.from({ length: locs.length() }, (_, i) => new EpubCFI(locs.cfiFromLocation(i)).spinePos)
      const pageOf = (href: string) => {
        const section = book!.spine.get(href)
        return section ? sectionStartPage(spineOfPage, section.index) : undefined
      }
      emitOutline(withPages(outline, pageOf))

      const cur = rendition.currentLocation() as unknown as Location | undefined
      if (cur?.start) report(cur)
    })().catch((e: Error) => {
      if (abort.signal.aborted || e.name === 'AbortError') return
      setError(e.message)
      setLoading(false)
    })

    // epub.js only re-measures on window resize, but the host also changes
    // size when the bookmarks panel opens or the layout switches.
    let last = { w: 0, h: 0 }
    let frame = 0
    const ro = new ResizeObserver(([entry]) => {
      const w = Math.round(entry.contentRect.width)
      const h = Math.round(entry.contentRect.height)
      const changed = last.w !== 0 && (w !== last.w || h !== last.h)
      last = { w, h }
      const r = renditionRef.current
      if (!changed || !r) return
      cancelAnimationFrame(frame)
      // Called with no size it re-reads the container; the typings wrongly
      // mark both arguments as required.
      frame = requestAnimationFrame(() => (r.resize as (w?: number, h?: number) => void).call(r))
    })
    ro.observe(host)

    return () => {
      abort.abort()
      ro.disconnect()
      cancelAnimationFrame(frame)
      controllerRef.current = null
      renditionRef.current = null
      book?.destroy()
    }
  }, [fileId, controllerRef])

  useEffect(() => {
    renditionRef.current?.spread(spreadMode(spread), 0)
  }, [spread])

  return (
    <div className="epub-stage">
      {error && <div className="notice notice-error">Could not open this EPUB: {error}</div>}
      {loading && !error && <div className="notice">Opening book…</div>}
      <div className={`epub-host ${spread === 1 ? 'is-single' : ''} ${loading ? 'is-loading' : ''}`} ref={hostRef} />
      {showPageNumbers && shownPages && !loading && (
        <div className="epub-pagenum">
          {shownPages[0] === shownPages[1] ? shownPages[0] : `${shownPages[0]}–${shownPages[1]}`}
        </div>
      )}
    </div>
  )
}
