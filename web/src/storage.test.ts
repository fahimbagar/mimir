import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getLocations, getPageNumbers, getSpread, setLocations, setPageNumbers, setSpread } from './storage'

function memoryStorage(): Storage {
  const m = new Map<string, string>()
  return {
    get length() {
      return m.size
    },
    clear: () => m.clear(),
    getItem: (k) => m.get(k) ?? null,
    key: (i) => [...m.keys()][i] ?? null,
    removeItem: (k) => void m.delete(k),
    setItem: (k, v) => void m.set(k, String(v)),
  }
}

describe('storage', () => {
  beforeEach(() => vi.stubGlobal('localStorage', memoryStorage()))
  afterEach(() => vi.unstubAllGlobals())

  it('defaults to two pages and remembers the choice', () => {
    expect(getSpread()).toBe(2)
    setSpread(1)
    expect(getSpread()).toBe(1)
  })

  it('shows page numbers until turned off', () => {
    expect(getPageNumbers()).toBe(true)
    setPageNumbers(false)
    expect(getPageNumbers()).toBe(false)
  })

  it('caches EPUB locations per file', () => {
    setLocations('f1', '["epubcfi(/6/2)"]')
    expect(getLocations('f1')).toBe('["epubcfi(/6/2)"]')
    expect(getLocations('f2')).toBeNull()
  })

  it('falls back to defaults when storage is corrupt or blocked', () => {
    localStorage.setItem('mimir:spread', '{not json')
    expect(getSpread()).toBe(2)

    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
    })
    expect(getSpread()).toBe(2)
    expect(() => setSpread(1)).not.toThrow()
  })
})
