// Two-page layout follows a printed book: the cover sits alone on the right,
// then 2–3, 4–5, ...
export function pagesInView(page: number, total: number, spread: 1 | 2): number[] {
  if (spread === 1 || page === 1) return [page]
  const left = page % 2 === 0 ? page : page - 1
  return left + 1 <= total ? [left, left + 1] : [left]
}

export function nextPage(page: number, total: number, spread: 1 | 2): number {
  const view = pagesInView(page, total, spread)
  return Math.min(total, view[view.length - 1] + 1)
}

export function prevPage(page: number, total: number, spread: 1 | 2): number {
  return Math.max(1, pagesInView(page, total, spread)[0] - 1)
}
