// Reading state lives in this browser only. Storage can be unavailable
// (private windows, blocked site data), so every access is guarded and the
// app keeps working without it.

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

// PDF: page number. EPUB: CFI string.
export const getPosition = <T extends number | string>(fileId: string) => read<T>(`pos:${fileId}`)
export const setPosition = (fileId: string, pos: number | string) => write(`pos:${fileId}`, pos)

export const getLocations = (fileId: string) => read<string>(`locs:${fileId}`)
export const setLocations = (fileId: string, json: string) => write(`locs:${fileId}`, json)

export interface RecentEntry {
  fileId: string
  progress: number
  at: number
}

const MAX_RECENT = 12

export const getRecent = (): RecentEntry[] => read<RecentEntry[]>('recent') ?? []

export function touchRecent(fileId: string, progress?: number) {
  const list = getRecent()
  const prev = list.find((e) => e.fileId === fileId)
  const entry: RecentEntry = { fileId, progress: progress ?? prev?.progress ?? 0, at: Date.now() }
  write('recent', [entry, ...list.filter((e) => e.fileId !== fileId)].slice(0, MAX_RECENT))
}
