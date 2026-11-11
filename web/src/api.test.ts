import { afterEach, describe, expect, it, vi } from 'vitest'
import { findFile, formatSize, loadBooks, type Book } from './api'

const books: Book[] = [
  {
    id: 'b1',
    title: 'One',
    addedAt: '',
    files: [
      { id: 'e1', format: 'epub', size: 1, name: 'one.epub', hasCover: true, modTime: '' },
      { id: 'p1', format: 'pdf', size: 1, name: 'one.pdf', hasCover: false, modTime: '' },
    ],
  },
]

describe('findFile', () => {
  it('returns the book and file for a file id', () => {
    const hit = findFile(books, 'p1')
    expect(hit?.book.id).toBe('b1')
    expect(hit?.file.format).toBe('pdf')
    expect(findFile(books, 'nope')).toBeNull()
  })
})

describe('formatSize', () => {
  it('uses KB below a megabyte and never shows 0 KB', () => {
    expect(formatSize(10)).toBe('1 KB')
    expect(formatSize(512 * 1024)).toBe('512 KB')
    expect(formatSize(56_154_074)).toBe('53.6 MB')
  })
})

describe('loadBooks', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('caches the list until asked to refresh', async () => {
    const fetchMock = vi.fn(() => Promise.resolve(new Response(JSON.stringify(books))))
    vi.stubGlobal('fetch', fetchMock)
    await loadBooks(true)
    await loadBooks()
    expect(fetchMock).toHaveBeenCalledTimes(1)
    await loadBooks(true)
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it('reports HTTP errors and retries on the next call', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response('boom', { status: 500 }))
      .mockResolvedValueOnce(new Response(JSON.stringify(books)))
    vi.stubGlobal('fetch', fetchMock)
    await expect(loadBooks(true)).rejects.toThrow('HTTP 500')
    await expect(loadBooks()).resolves.toHaveLength(1)
  })
})
