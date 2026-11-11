import { getDocument, GlobalWorkerOptions } from 'pdfjs-dist'
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url'
import { fileUrl } from './api'

GlobalWorkerOptions.workerSrc = workerUrl

// Range requests only: some books here are 60 MB and we rarely need all of it.
export function openPdf(fileId: string) {
  return getDocument({
    url: fileUrl(fileId),
    disableAutoFetch: true,
    disableStream: true,
    rangeChunkSize: 256 * 1024,
  })
}

// Thumbnails: at most two PDFs open at once so a shelf of big files doesn't
// stall the reader or the network.
const MAX_ACTIVE = 2
let active = 0
const waiting: Array<() => void> = []

async function withSlot<T>(fn: () => Promise<T>): Promise<T> {
  if (active >= MAX_ACTIVE) await new Promise<void>((r) => waiting.push(r))
  else active++
  try {
    return await fn()
  } finally {
    // Hand the slot straight to the next waiter so nobody can jump the queue.
    const next = waiting.shift()
    if (next) next()
    else active--
  }
}

const thumbs = new Map<string, Promise<string>>()

export function pdfThumbnail(fileId: string, width = 360): Promise<string> {
  let p = thumbs.get(fileId)
  if (!p) {
    p = withSlot(async () => {
      const task = openPdf(fileId)
      try {
        const doc = await task.promise
        const page = await doc.getPage(1)
        const base = page.getViewport({ scale: 1 })
        const viewport = page.getViewport({ scale: width / base.width })
        const canvas = document.createElement('canvas')
        canvas.width = Math.floor(viewport.width)
        canvas.height = Math.floor(viewport.height)
        await page.render({ canvas, viewport }).promise
        const blob = await new Promise<Blob | null>((r) => canvas.toBlob(r, 'image/jpeg', 0.85))
        if (!blob) throw new Error('thumbnail encode failed')
        return URL.createObjectURL(blob)
      } finally {
        await task.destroy()
      }
    })
    thumbs.set(fileId, p)
  }
  return p
}
