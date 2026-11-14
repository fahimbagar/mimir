import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import type { PDFDocumentProxy, RenderTask } from 'pdfjs-dist'
import { openPdf } from '../pdf'
import { flattenOutline } from './outline'
import { nextPage, pagesInView, prevPage } from './spread'
import type { OutlineItem, ReaderProps } from './types'

// Breathing room around the page(s); facing pages touch like a real spread.
const PAD = 24
// Room for the page number caption under each page.
const NUM_SPACE = 26

type PdfOutlineNode = Awaited<ReturnType<PDFDocumentProxy['getOutline']>>[number]
type PdfRef = Parameters<PDFDocumentProxy['getPageIndex']>[0]

const isPdfRef = (v: unknown): v is PdfRef =>
  typeof v === 'object' && v !== null && typeof (v as PdfRef).num === 'number' && typeof (v as PdfRef).gen === 'number'

async function destPage(doc: PDFDocumentProxy, dest: PdfOutlineNode['dest']): Promise<number | undefined> {
  if (!dest) return undefined
  try {
    const explicit: unknown[] | null = typeof dest === 'string' ? await doc.getDestination(dest) : dest
    const ref = explicit?.[0]
    // Most PDFs point at a page object; a few store a bare page index.
    if (typeof ref === 'number') return ref + 1
    if (isPdfRef(ref)) return (await doc.getPageIndex(ref)) + 1
    return undefined
  } catch {
    // A dangling destination is common in scanned books. The entry stays in
    // the list but can't be opened.
    return undefined
  }
}

async function loadOutline(doc: PDFDocumentProxy): Promise<OutlineItem[]> {
  // Typed as an array, but pdf.js returns null when there is no outline.
  const root = (await doc.getOutline()) as PdfOutlineNode[] | null
  const convert = (nodes: PdfOutlineNode[], prefix: string): Promise<OutlineItem[]> =>
    Promise.all(
      nodes.map(async (n, i) => ({
        id: `${prefix}${i}`,
        label: n.title.trim() || 'Untitled',
        page: await destPage(doc, n.dest),
        // The typings elide the recursive type of children to any[].
        children: await convert((n.items ?? []) as PdfOutlineNode[], `${prefix}${i}.`),
      })),
    )
  return root ? convert(root, '') : []
}

export function PdfReader({ fileId, initialPosition, spread, controllerRef, onStatus, onOutline, showPageNumbers }: ReaderProps) {
  const stageRef = useRef<HTMLDivElement>(null)
  const pagesRef = useRef<HTMLDivElement>(null)
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(() => (typeof initialPosition === 'number' ? initialPosition : 1))
  const [size, setSize] = useState({ w: 0, h: 0 })
  const emitStatus = useEffectEvent(onStatus)
  const emitOutline = useEffectEvent(onOutline)
  // Flattened, in reading order, for finding the bookmark of the current page.
  const [outline, setOutline] = useState<OutlineItem[]>([])

  useEffect(() => {
    const task = openPdf(fileId)
    task.promise.then(
      (d) => {
        setDoc(d)
        setPage((p) => Math.min(Math.max(1, p), d.numPages))
      },
      (e: Error) => setError(e.message),
    )
    return () => {
      setDoc(null)
      void task.destroy()
    }
  }, [fileId])

  useEffect(() => {
    if (!doc) return
    let alive = true
    loadOutline(doc).then(
      (items) => {
        if (!alive) return
        setOutline(flattenOutline(items))
        emitOutline(items)
      },
      () => alive && emitOutline([]),
    )
    return () => {
      alive = false
    }
  }, [doc])

  useEffect(() => {
    const el = stageRef.current
    if (!el) return
    const ro = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect
      setSize({ w: Math.floor(width), h: Math.floor(height) })
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const total = doc?.numPages ?? 0
  // Keyed by content so moving within a spread (3 -> 2) doesn't re-render it.
  const viewKey = total ? pagesInView(page, total, spread).join(',') : ''
  const view = useMemo(() => (viewKey ? viewKey.split(',').map(Number) : []), [viewKey])

  useEffect(() => {
    if (!total) return
    const last = view[view.length - 1]
    let outlineId: string | undefined
    for (const it of outline) if (it.page != null && it.page <= last) outlineId = it.id
    emitStatus({
      label: view.length > 1 ? `Pages ${view[0]}–${last} of ${total}` : `Page ${view[0]} of ${total}`,
      position: page,
      progress: total > 1 ? (last - 1) / (total - 1) : 1,
      page: view[0],
      pages: total,
      outlineId,
    })
  }, [page, total, view, outline])

  useEffect(() => {
    if (!total) return
    controllerRef.current = {
      next: () => setPage((p) => nextPage(p, total, spread)),
      prev: () => setPage((p) => prevPage(p, total, spread)),
      goTo: (item) => item.page != null && setPage(item.page),
      goToPage: (n) => setPage(Math.min(total, Math.max(1, Math.round(n)))),
    }
    return () => {
      controllerRef.current = null
    }
  }, [total, spread, controllerRef])

  useEffect(() => {
    if (!doc || !size.w || !size.h || view.length === 0) return
    let cancelled = false
    const tasks: RenderTask[] = []

    ;(async () => {
      const pages = await Promise.all(view.map((n) => doc.getPage(n)))
      if (cancelled) return
      const availW = (size.w - PAD * 2) / pages.length
      const availH = size.h - PAD * 2 - (showPageNumbers ? NUM_SPACE : 0)
      // One scale for the whole spread keeps facing pages the same height.
      const scale = Math.min(
        ...pages.map((p) => {
          const v = p.getViewport({ scale: 1 })
          return Math.min(availW / v.width, availH / v.height)
        }),
      )
      const dpr = window.devicePixelRatio || 1

      // Render off-screen, then swap in, so turning a page never flashes blank.
      const rendered = await Promise.all(
        pages.map(async (p, i) => {
          const css = p.getViewport({ scale })
          const vp = p.getViewport({ scale: scale * dpr })
          const canvas = document.createElement('canvas')
          canvas.className = 'pdf-page'
          canvas.width = Math.floor(vp.width)
          canvas.height = Math.floor(vp.height)
          canvas.style.width = `${Math.floor(css.width)}px`
          canvas.style.height = `${Math.floor(css.height)}px`
          const task = p.render({ canvas, viewport: vp })
          tasks.push(task)
          await task.promise
          const wrap = document.createElement('div')
          wrap.className = 'pdf-page-wrap'
          wrap.append(canvas)
          if (showPageNumbers) {
            const num = document.createElement('span')
            num.className = 'pdf-page-num'
            num.textContent = String(view[i])
            wrap.append(num)
          }
          return wrap
        }),
      )
      if (!cancelled) pagesRef.current?.replaceChildren(...rendered)

      // Warm the next spread while the reader is looking at this one.
      const ahead = view[view.length - 1] + 1
      for (let n = ahead; n < ahead + pages.length && n <= doc.numPages; n++) void doc.getPage(n)
    })().catch((e: Error) => {
      if (!cancelled && e.name !== 'RenderingCancelledException') setError(e.message)
    })

    return () => {
      cancelled = true
      tasks.forEach((t) => t.cancel())
    }
  }, [doc, size.w, size.h, view, showPageNumbers])

  return (
    <div className="pdf-stage" ref={stageRef}>
      {error ? (
        <div className="notice notice-error">Could not open this PDF: {error}</div>
      ) : !doc ? (
        <div className="notice">Opening book…</div>
      ) : null}
      <div className={`pdf-spread ${view.length > 1 ? 'is-two' : ''}`} ref={pagesRef} />
    </div>
  )
}
