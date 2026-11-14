export type Format = 'pdf' | 'epub'

export interface BookFile {
  id: string
  format: Format
  size: number
  name: string
  hasCover: boolean
  modTime: string
}

export interface Book {
  id: string
  title: string
  author?: string
  files: BookFile[]
  addedAt: string
}

export interface User {
  email: string
}

let onUnauthorized: (() => void) | null = null

// Called when any API request finds the session gone (expired, revoked, or
// removed from the allowlist), so the app can show the login page.
export function setUnauthorizedHandler(fn: (() => void) | null) {
  onUnauthorized = fn
}

async function api(path: string, init?: RequestInit): Promise<Response> {
  const res = await fetch(path, init)
  if (res.status === 401) {
    onUnauthorized?.()
    throw new Error('Please sign in again.')
  }
  return res
}

export async function getMe(): Promise<User | null> {
  const res = await fetch('/api/me')
  if (res.status === 401) return null
  if (!res.ok) throw new Error(`Could not reach the library (HTTP ${res.status})`)
  return (await res.json()) as User
}

export async function signOut(): Promise<void> {
  const res = await fetch('/auth/logout', { method: 'POST' })
  if (!res.ok) throw new Error(`Sign out failed (HTTP ${res.status})`)
  cached = null
}

let cached: Promise<Book[]> | null = null

export function loadBooks(refresh = false): Promise<Book[]> {
  if (!cached || refresh) {
    cached = api('/api/books').then(async (res) => {
      if (!res.ok) throw new Error(`Could not load library (HTTP ${res.status})`)
      return (await res.json()) as Book[]
    })
    cached.catch(() => {
      cached = null
    })
  }
  return cached
}

export const fileUrl = (fileId: string) => `/api/files/${fileId}`
export const coverUrl = (fileId: string) => `/api/files/${fileId}/cover`

export function findFile(books: Book[], fileId: string): { book: Book; file: BookFile } | null {
  for (const book of books) {
    const file = book.files.find((f) => f.id === fileId)
    if (file) return { book, file }
  }
  return null
}

export function formatSize(bytes: number): string {
  if (bytes < 1024 * 1024) return `${Math.max(1, Math.round(bytes / 1024))} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}
