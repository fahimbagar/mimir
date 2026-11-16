import { describe, expect, it } from 'vitest'
import { clampAxis, MAX_ZOOM, zoomAt, type View } from './panzoom'

describe('clampAxis', () => {
  it('centres content that fits', () => {
    expect(clampAxis(-50, 200, 300)).toBe(50)
  })

  it('stops larger content at either edge', () => {
    expect(clampAxis(20, 500, 300)).toBe(0)
    expect(clampAxis(-400, 500, 300)).toBe(-200)
    expect(clampAxis(-120, 500, 300)).toBe(-120)
  })
})

describe('zoomAt', () => {
  it('keeps the point under the cursor fixed', () => {
    const v: View = { zoom: 1, rendered: 1, x: 10, y: 20 }
    // Content point under (110, 70) before zooming, in zoom-1 units.
    const u = { x: (110 - v.x) / v.zoom, y: (70 - v.y) / v.zoom }
    zoomAt(v, 2.5, 110, 70)
    expect(v.zoom).toBe(2.5)
    expect(v.x + u.x * v.zoom).toBeCloseTo(110)
    expect(v.y + u.y * v.zoom).toBeCloseTo(70)
  })

  it('stays between fit and the maximum', () => {
    const v: View = { zoom: 2, rendered: 2, x: 0, y: 0 }
    zoomAt(v, 0.3, 0, 0)
    expect(v.zoom).toBe(1)
    zoomAt(v, 99, 0, 0)
    expect(v.zoom).toBe(MAX_ZOOM)
  })
})
