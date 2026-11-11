import { describe, expect, it } from 'vitest'
import { nextPage, pagesInView, prevPage } from './spread'

describe('pagesInView', () => {
  it('shows one page in single mode', () => {
    expect(pagesInView(7, 10, 1)).toEqual([7])
  })

  it('keeps the cover alone in two-page mode', () => {
    expect(pagesInView(1, 10, 2)).toEqual([1])
  })

  it('pairs even-odd pages like a printed book', () => {
    expect(pagesInView(2, 10, 2)).toEqual([2, 3])
    expect(pagesInView(3, 10, 2)).toEqual([2, 3])
    expect(pagesInView(9, 10, 2)).toEqual([8, 9])
  })

  it('shows a trailing even page alone', () => {
    expect(pagesInView(10, 10, 2)).toEqual([10])
  })
})

describe('page turning', () => {
  it('steps one page in single mode and clamps at the ends', () => {
    expect(nextPage(1, 3, 1)).toBe(2)
    expect(nextPage(3, 3, 1)).toBe(3)
    expect(prevPage(1, 3, 1)).toBe(1)
  })

  it('steps whole spreads in two-page mode', () => {
    expect(nextPage(1, 10, 2)).toBe(2)
    expect(nextPage(2, 10, 2)).toBe(4)
    expect(nextPage(3, 10, 2)).toBe(4)
    expect(prevPage(4, 10, 2)).toBe(3) // lands on the 2–3 spread
    expect(pagesInView(prevPage(4, 10, 2), 10, 2)).toEqual([2, 3])
    expect(prevPage(2, 10, 2)).toBe(1)
  })

  it('stays put on the last spread', () => {
    expect(nextPage(9, 9, 2)).toBe(9)
    expect(nextPage(10, 10, 2)).toBe(10)
  })
})
