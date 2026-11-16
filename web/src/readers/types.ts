import type { RefObject } from 'react'
import type { Spread } from '../storage'

// One entry of a book's built-in bookmarks: the PDF outline or EPUB TOC.
export interface OutlineItem {
  id: string
  label: string
  page?: number
  href?: string // EPUB only
  children: OutlineItem[]
}

export interface ReaderController {
  next(): void
  prev(): void
  goTo(item: OutlineItem): void
  goToPage(page: number): void
  zoom?(step: 1 | -1 | 0): void // in, out, or back to fit
}

export interface ReaderStatus {
  label: string
  position: number | string // what to save: PDF page or EPUB CFI
  progress: number | null // 0..1, null while an EPUB is still being measured
  page?: number // PDF: real page. EPUB: computed page, once known.
  pages?: number
  outlineId?: string // the bookmark the reader is currently inside
}

export interface ReaderProps {
  fileId: string
  initialPosition?: number | string // from the server; PDF page or EPUB CFI
  spread: Spread // already reduced to 1 when the reading area is too narrow
  controllerRef: RefObject<ReaderController | null>
  onStatus: (s: ReaderStatus) => void
  onOutline: (items: OutlineItem[]) => void
  showPageNumbers: boolean
  compact?: boolean // full screen or a small screen: trim the margins around pages
  onZoom?: (zoom: number) => void // 1 = fit; reported once a gesture pauses
}

// Below this reading-area width two pages are unreadable, so the reader page
// hides the two-page option and passes spread=1.
export const MIN_SPREAD_WIDTH = 720
