// Per-device preferences and caches; reading progress lives on the server.
// Storage can be unavailable (private windows, blocked site data), so every
// access is guarded and the app keeps working without it.

function read<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(`mimir:${key}`)
    return raw == null ? null : (JSON.parse(raw) as T)
  } catch {
    return null
  }
}

function write(key: string, value: unknown) {
  try {
    localStorage.setItem(`mimir:${key}`, JSON.stringify(value))
  } catch {
    // Quota exceeded or storage disabled: losing a bookmark is acceptable.
  }
}

export type Spread = 1 | 2

export const getSpread = (): Spread => (read<number>('spread') === 1 ? 1 : 2)
export const setSpread = (s: Spread) => write('spread', s)

// Default: open on screens wide enough to keep the page readable beside it.
export const getTocOpen = () => read<boolean>('toc') ?? window.innerWidth >= 1100
export const setTocOpen = (open: boolean) => write('toc', open)

export const getPageNumbers = () => read<boolean>('pagenums') ?? true
export const setPageNumbers = (show: boolean) => write('pagenums', show)

export const getLocations = (fileId: string) => read<string>(`locs:${fileId}`)
export const setLocations = (fileId: string, json: string) => write(`locs:${fileId}`, json)
