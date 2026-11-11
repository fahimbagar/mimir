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
}

export interface ReaderStatus {
  label: string
  progress: number // 0..1
  page?: number // PDF: real page. EPUB: computed page, once known.
  pages?: number
  outlineId?: string // the bookmark the reader is currently inside
}

export interface ReaderProps {
  fileId: string
  spread: Spread // already reduced to 1 when the reading area is too narrow
  controllerRef: RefObject<ReaderController | null>
  onStatus: (s: ReaderStatus) => void
  onOutline: (items: OutlineItem[]) => void
  showPageNumbers: boolean
}

// Below this reading-area width two pages are unreadable, so the reader page
// hides the two-page option and passes spread=1.
export const MIN_SPREAD_WIDTH = 720
