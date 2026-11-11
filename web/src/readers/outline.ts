import type { NavItem } from 'epubjs'
import type { OutlineItem } from './types'

export function flattenOutline(items: OutlineItem[], out: OutlineItem[] = []): OutlineItem[] {
  for (const it of items) {
    out.push(it)
    flattenOutline(it.children, out)
  }
  return out
}

// IDs encode the path ("2.0.1") so the panel can unfold an entry's ancestors.
export function fromEpubToc(items: NavItem[], prefix = ''): OutlineItem[] {
  return items.map((it, i) => ({
    id: `${prefix}${i}`,
    label: it.label.trim() || 'Untitled',
    href: it.href,
    children: fromEpubToc(it.subitems ?? [], `${prefix}${i}.`),
  }))
}

export function withPages(items: OutlineItem[], pageOf: (href: string) => number | undefined): OutlineItem[] {
  return items.map((it) => ({
    ...it,
    page: it.href ? pageOf(it.href) : undefined,
    children: withPages(it.children, pageOf),
  }))
}

const baseHref = (h: string) => h.split('#')[0].replace(/^\.?\//, '')

// TOC hrefs are relative to the nav document while locations report hrefs
// relative to the package, so compare by path suffix.
export function itemForHref(flat: OutlineItem[], href: string): OutlineItem | undefined {
  const target = baseHref(href)
  return flat.find((t) => {
    const b = baseHref(t.href ?? '')
    return b !== '' && (b === target || target.endsWith('/' + b) || b.endsWith('/' + target))
  })
}

// spineOfPage[i] is the spine section that page i+1 starts in (non-decreasing).
// Returns the 1-based first page of the given section, or the last page if the
// section comes after every page.
export function sectionStartPage(spineOfPage: number[], sectionIndex: number): number | undefined {
  if (spineOfPage.length === 0) return undefined
  let lo = 0
  let hi = spineOfPage.length
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (spineOfPage[mid] < sectionIndex) lo = mid + 1
    else hi = mid
  }
  return Math.min(lo + 1, spineOfPage.length)
}
