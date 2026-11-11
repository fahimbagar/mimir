import { describe, expect, it } from 'vitest'
import { flattenOutline, fromEpubToc, itemForHref, sectionStartPage, withPages } from './outline'

const toc = [
  { id: 'a', href: 'Text/cover.xhtml', label: ' Cover ' },
  {
    id: 'b',
    href: 'Text/part1.xhtml',
    label: 'Part I',
    subitems: [
      { id: 'c', href: 'Text/ch01.xhtml#start', label: 'Chapter 1' },
      { id: 'd', href: 'Text/ch02.xhtml', label: '' },
    ],
  },
]

describe('fromEpubToc', () => {
  it('builds path ids, trims labels and names blank entries', () => {
    const out = fromEpubToc(toc)
    expect(out.map((i) => [i.id, i.label])).toEqual([
      ['0', 'Cover'],
      ['1', 'Part I'],
    ])
    expect(out[1].children.map((i) => [i.id, i.label])).toEqual([
      ['1.0', 'Chapter 1'],
      ['1.1', 'Untitled'],
    ])
  })
})

describe('flattenOutline', () => {
  it('lists entries in reading order, parents before children', () => {
    expect(flattenOutline(fromEpubToc(toc)).map((i) => i.id)).toEqual(['0', '1', '1.0', '1.1'])
  })
})

describe('withPages', () => {
  it('fills pages through the whole tree', () => {
    const pages: Record<string, number> = { 'Text/cover.xhtml': 1, 'Text/part1.xhtml': 4, 'Text/ch01.xhtml#start': 5 }
    const out = flattenOutline(withPages(fromEpubToc(toc), (h) => pages[h]))
    expect(out.map((i) => i.page)).toEqual([1, 4, 5, undefined])
  })
})

describe('itemForHref', () => {
  const flat = flattenOutline(fromEpubToc(toc))

  it('matches across different base folders and ignores fragments', () => {
    expect(itemForHref(flat, 'OEBPS/Text/ch01.xhtml')?.id).toBe('1.0')
    expect(itemForHref(flat, 'ch02.xhtml')?.id).toBe('1.1')
  })

  it('returns nothing for files outside the TOC', () => {
    expect(itemForHref(flat, 'Text/notes.xhtml')).toBeUndefined()
  })
})

describe('sectionStartPage', () => {
  // pages 1-2 in section 0, 3-5 in section 2, 6 in section 3
  const spineOfPage = [0, 0, 2, 2, 2, 3]

  it('returns the first page of a section', () => {
    expect(sectionStartPage(spineOfPage, 0)).toBe(1)
    expect(sectionStartPage(spineOfPage, 2)).toBe(3)
    expect(sectionStartPage(spineOfPage, 3)).toBe(6)
  })

  it('maps a section with no text of its own to the next page', () => {
    expect(sectionStartPage(spineOfPage, 1)).toBe(3)
  })

  it('clamps sections past the end and handles an empty book', () => {
    expect(sectionStartPage(spineOfPage, 9)).toBe(6)
    expect(sectionStartPage([], 0)).toBeUndefined()
  })
})
