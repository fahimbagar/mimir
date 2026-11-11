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

let cached: Promise<Book[]> | null = null

export function loadBooks(refresh = false): Promise<Book[]> {
  if (!cached || refresh) {
    cached = fetch('/api/books').then(async (res) => {
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
