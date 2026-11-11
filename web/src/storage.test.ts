import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { getPosition, getRecent, getSpread, setPosition, setSpread, touchRecent } from './storage'

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

  it('round-trips positions of either type', () => {
    setPosition('pdf', 42)
    setPosition('epub', 'epubcfi(/6/4!/4/2)')
    expect(getPosition<number>('pdf')).toBe(42)
    expect(getPosition<string>('epub')).toBe('epubcfi(/6/4!/4/2)')
    expect(getPosition('missing')).toBeNull()
  })

  it('keeps recent books newest first, without duplicates, capped at 12', () => {
    for (let i = 0; i < 14; i++) touchRecent(`f${i}`, i / 100)
    touchRecent('f5')
    const recent = getRecent()
    expect(recent).toHaveLength(12)
    expect(recent[0].fileId).toBe('f5')
    expect(recent[0].progress).toBe(0.05) // kept when no new progress is given
    expect(recent.filter((r) => r.fileId === 'f5')).toHaveLength(1)
    expect(recent.map((r) => r.fileId)).not.toContain('f0')
  })

  it('falls back to defaults when storage is unreadable or blocked', () => {
    localStorage.setItem('mimir:recent', '{not json')
    expect(getRecent()).toEqual([])

    vi.stubGlobal('localStorage', {
      getItem: () => {
        throw new Error('SecurityError')
      },
      setItem: () => {
        throw new Error('QuotaExceededError')
      },
    })
    expect(getSpread()).toBe(2)
    expect(() => touchRecent('x')).not.toThrow()
  })
})
