import { useEffect, useEffectEvent, useMemo, useRef, useState } from 'react'
import type { PDFDocumentProxy, RenderTask } from 'pdfjs-dist'
import { openPdf } from '../pdf'
import { flattenOutline } from './outline'
import { clampAxis, MAX_ZOOM, zoomAt, type View } from './panzoom'
import { nextPage, pagesInView, prevPage } from './spread'
import type { OutlineItem, ReaderProps } from './types'

// Breathing room around the page(s); facing pages touch like a real spread.
const PAD = 24
const PAD_COMPACT = 6
// Room for the page number caption under each page.
const NUM_SPACE = 26
// Magnifier: how much the lens enlarges, and its diameter in CSS pixels.
const MAGNIFY = 2.5
const LENS = 240
// Browsers refuse or blank canvases past roughly this many pixels.
const MAX_CANVAS_PIXELS = 16_777_216

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

// Positions the drawn spread for the current zoom and pan. Between a gesture
// and the redraw that follows it, the canvases are stretched by CSS.
function place(stage: HTMLElement, spread: HTMLElement, v: View) {
  const s = v.zoom / v.rendered
  v.x = clampAxis(v.x, spread.offsetWidth * s, stage.clientWidth)
  v.y = clampAxis(v.y, spread.offsetHeight * s, stage.clientHeight)
  spread.style.transform = `translate(${v.x}px, ${v.y}px) scale(${s})`
  stage.classList.toggle('is-zoomed', v.zoom > 1)
}

// A drag that starts on a page-turn zone pans the page; the click that the
// browser sends when it ends must not turn the page as well.
function swallowNextClick() {
  const stop = (e: Event) => {
    e.stopPropagation()
    e.preventDefault()
  }
  window.addEventListener('click', stop, { capture: true, once: true })
  window.setTimeout(() => window.removeEventListener('click', stop, { capture: true }), 0)
}

interface PdfReaderProps extends ReaderProps {
  magnify?: boolean
}

export function PdfReader({
  fileId,
  initialPosition,
  spread,
  controllerRef,
  onStatus,
  onOutline,
  showPageNumbers,
  compact = false,
  magnify = false,
  onZoom,
}: PdfReaderProps) {
  const stageRef = useRef<HTMLDivElement>(null)
  const pagesRef = useRef<HTMLDivElement>(null)
  const lensRef = useRef<HTMLCanvasElement>(null)
  const [doc, setDoc] = useState<PDFDocumentProxy | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [page, setPage] = useState(() => (typeof initialPosition === 'number' ? initialPosition : 1))
  const [size, setSize] = useState({ w: 0, h: 0 })
  const emitStatus = useEffectEvent(onStatus)
  const emitOutline = useEffectEvent(onOutline)
  const emitZoom = useEffectEvent((zoom: number) => onZoom?.(zoom))
  const viewRef = useRef<View>({ zoom: 1, rendered: 1, x: 0, y: 0 })
  const shownRef = useRef('')
  const zoomStepRef = useRef<(step: 1 | -1 | 0) => void>(() => {})
  // The zoom the next redraw uses; it follows gestures once they pause.
  const [renderZoom, setRenderZoom] = useState(1)
  // Flattened, in reading order, for finding the bookmark of the current page.
  const [outline, setOutline] = useState<OutlineItem[]>([])

  useEffect(() => {
    let alive = true
    const task = openPdf(fileId)
    task.promise.then(
      (d) => {
        if (!alive) return
        setDoc(d)
        setPage((p) => Math.min(Math.max(1, p), d.numPages))
      },
      // destroy() below rejects the load with "Loading aborted"; that's us
      // leaving, not a broken file.
      (e: Error) => alive && setError(e.message),
    )
    return () => {
      alive = false
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
      zoom: (step) => zoomStepRef.current(step),
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
      const pad = compact ? PAD_COMPACT : PAD
      const availW = (size.w - pad * 2) / pages.length
      const availH = size.h - pad * 2 - (showPageNumbers ? NUM_SPACE : 0)
      // One scale for the whole spread keeps facing pages the same height.
      const scale = Math.min(
        ...pages.map((p) => {
          const v = p.getViewport({ scale: 1 })
          return Math.min(availW / v.width, availH / v.height)
        }),
      )
      const dpr = window.devicePixelRatio || 1
      const zoom = renderZoom

      // Render off-screen, then swap in, so turning a page never flashes blank.
      const rendered = await Promise.all(
        pages.map(async (p, i) => {
          const css = p.getViewport({ scale: scale * zoom })
          // With the magnifier on, draw the page at lens resolution so the
          // lens copies real pixels instead of stretching screen ones. Past
          // the canvas size limit, zoomed pages get softer instead of blank.
          const density = Math.min(
            dpr * (magnify ? MAGNIFY : 1),
            Math.sqrt(MAX_CANVAS_PIXELS / (css.width * css.height)),
          )
          const vp = p.getViewport({ scale: scale * zoom * density })
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
      const stage = stageRef.current
      const spreadEl = pagesRef.current
      if (!cancelled && stage && spreadEl) {
        spreadEl.replaceChildren(...rendered)
        const v = viewRef.current
        v.rendered = zoom
        // A new page keeps the zoom and starts at its top.
        const key = view.join(',')
        if (shownRef.current !== key) {
          shownRef.current = key
          v.y = 0
        }
        place(stage, spreadEl, v)
      }

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
  }, [doc, size.w, size.h, view, showPageNumbers, compact, magnify, renderZoom])

  // Wheel and pinch zoom, drag to pan, double click or tap to switch between
  // fit and 2x. Listeners sit on window because on phones and in full screen
  // the page-turn zones lie over the page, and gestures there count too.
  useEffect(() => {
    const stage = stageRef.current
    const spreadEl = pagesRef.current
    if (!stage || !spreadEl) return
    const v = viewRef.current
    let requested = v.rendered
    let settle = 0

    const ready = () => spreadEl.childElementCount > 0
    const ours = (t: EventTarget | null) => t instanceof Element && (stage.contains(t) || t.closest('.turn') !== null)
    const local = (e: { clientX: number; clientY: number }) => {
      const r = stage.getBoundingClientRect()
      return { x: e.clientX - r.left, y: e.clientY - r.top }
    }
    // Redraw sharp at the new zoom once the gesture pauses.
    const update = () => {
      place(stage, spreadEl, v)
      window.clearTimeout(settle)
      settle = window.setTimeout(() => {
        if (Math.abs(v.zoom - requested) > 0.01) {
          requested = v.zoom
          setRenderZoom(v.zoom)
        }
        emitZoom(v.zoom)
      }, 150)
    }

    zoomStepRef.current = (step) => {
      if (!ready()) return
      zoomAt(v, step === 0 ? 1 : v.zoom * (step > 0 ? 1.25 : 0.8), stage.clientWidth / 2, stage.clientHeight / 2)
      update()
    }

    const onWheel = (e: WheelEvent) => {
      if (!ours(e.target) || !ready()) return
      e.preventDefault()
      const dy = e.deltaMode === WheelEvent.DOM_DELTA_LINE ? e.deltaY * 16 : e.deltaY
      const p = local(e)
      // Trackpad pinches arrive as ctrl+wheel with much smaller steps.
      const factor = Math.exp(-dy * (e.ctrlKey ? 0.01 : 0.002))
      zoomAt(v, v.zoom * factor, Math.min(stage.clientWidth, Math.max(0, p.x)), p.y)
      update()
    }

    const pts = new Map<number, { x: number; y: number }>()
    let base: { x: number; y: number; zoom: number; mx: number; my: number; dist: number } | null = null
    let start = { x: 0, y: 0 }
    let moved = false
    let fromTurn = false
    let lastTap = { t: 0, x: 0, y: 0 }

    const centre = () => {
      const [a, b] = [...pts.values()]
      return b ? { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, dist: Math.hypot(a.x - b.x, a.y - b.y) } : { ...a, dist: 0 }
    }
    // Gestures are measured from the moment the finger count last changed.
    const rebase = () => {
      if (pts.size === 0) {
        base = null
        return
      }
      const c = centre()
      base = { x: v.x, y: v.y, zoom: v.zoom, mx: c.x, my: c.y, dist: c.dist }
    }

    const onDown = (e: PointerEvent) => {
      if (e.pointerType === 'mouse' && e.button !== 0) return
      if (!ours(e.target) || !ready()) return
      const p = local(e)
      if (pts.size === 0) {
        moved = false
        fromTurn = (e.target as Element).closest('.turn') !== null
        start = p
      }
      pts.set(e.pointerId, p)
      rebase()
    }

    const onMove = (e: PointerEvent) => {
      if (!base || !pts.has(e.pointerId)) return
      pts.set(e.pointerId, local(e))
      const c = centre()
      if (!moved && pts.size < 2 && Math.hypot(c.x - start.x, c.y - start.y) < 8) return
      moved = true
      const pinching = pts.size >= 2 && base.dist > 0
      // At fit there's nothing to pan.
      if (!pinching && v.zoom <= 1) return
      const z = pinching ? Math.min(MAX_ZOOM, Math.max(1, (base.zoom * c.dist) / base.dist)) : base.zoom
      v.x = c.x - ((base.mx - base.x) * z) / base.zoom
      v.y = c.y - ((base.my - base.y) * z) / base.zoom
      v.zoom = z
      stage.classList.add('is-panning')
      update()
    }

    const onUp = (e: PointerEvent) => {
      if (!pts.delete(e.pointerId)) return
      rebase()
      if (pts.size > 0) return
      stage.classList.remove('is-panning')
      if (moved) {
        if (fromTurn) swallowNextClick()
        return
      }
      if (fromTurn || e.type === 'pointercancel') return
      const p = local(e)
      if (e.timeStamp - lastTap.t < 300 && Math.hypot(p.x - lastTap.x, p.y - lastTap.y) < 30) {
        zoomAt(v, v.zoom > 1 ? 1 : 2, p.x, p.y)
        update()
        lastTap = { t: 0, x: 0, y: 0 }
      } else {
        lastTap = { t: e.timeStamp, x: p.x, y: p.y }
      }
    }

    window.addEventListener('wheel', onWheel, { passive: false })
    window.addEventListener('pointerdown', onDown)
    window.addEventListener('pointermove', onMove)
    window.addEventListener('pointerup', onUp)
    window.addEventListener('pointercancel', onUp)
    return () => {
      window.clearTimeout(settle)
      window.removeEventListener('wheel', onWheel)
      window.removeEventListener('pointerdown', onDown)
      window.removeEventListener('pointermove', onMove)
      window.removeEventListener('pointerup', onUp)
      window.removeEventListener('pointercancel', onUp)
      zoomStepRef.current = () => {}
    }
  }, [])

  useEffect(() => {
    const lens = lensRef.current
    if (!magnify || !lens) return
    const ctx = lens.getContext('2d')
    if (!ctx) return
    const dpr = window.devicePixelRatio || 1
    lens.width = lens.height = Math.round(LENS * dpr)
    let frame = 0
    let x = 0
    let y = 0

    const hide = () => {
      lens.hidden = true
    }
    const draw = () => {
      frame = 0
      const pages = pagesRef.current?.querySelectorAll<HTMLCanvasElement>('canvas.pdf-page') ?? []
      for (const page of pages) {
        const r = page.getBoundingClientRect()
        if (x < r.left || x > r.right || y < r.top || y > r.bottom) continue
        // Page canvas pixels per CSS pixel on screen.
        const ratio = page.width / r.width
        const side = (LENS / MAGNIFY) * ratio
        ctx.clearRect(0, 0, lens.width, lens.height)
        ctx.drawImage(page, (x - r.left) * ratio - side / 2, (y - r.top) * ratio - side / 2, side, side, 0, 0, lens.width, lens.height)
        lens.style.transform = `translate(${x - LENS / 2}px, ${y - LENS / 2}px)`
        lens.hidden = false
        return
      }
      hide()
    }
    const onMove = (e: PointerEvent) => {
      if (e.pointerType !== 'mouse') return
      x = e.clientX
      y = e.clientY
      if (!frame) frame = requestAnimationFrame(draw)
    }

    window.addEventListener('pointermove', onMove)
    document.documentElement.addEventListener('mouseleave', hide)
    return () => {
      cancelAnimationFrame(frame)
      window.removeEventListener('pointermove', onMove)
      document.documentElement.removeEventListener('mouseleave', hide)
      hide()
    }
  }, [magnify])

  return (
    <div className={`pdf-stage ${magnify ? 'is-magnifying' : ''}`} ref={stageRef}>
      {error ? (
        <div className="notice notice-error">Could not open this PDF: {error}</div>
      ) : !doc ? (
        <div className="notice">Opening book…</div>
      ) : null}
      <div className={`pdf-spread ${view.length > 1 ? 'is-two' : ''}`} ref={pagesRef} />
      <canvas className="pdf-lens" ref={lensRef} hidden aria-hidden />
    </div>
  )
}
