// Zoom and pan state for the PDF reader. It lives outside React state so
// wheel and pinch gestures don't re-render the reader on every frame.

export const MAX_ZOOM = 4

export interface View {
  zoom: number // 1 = the page fits the stage
  rendered: number // zoom the canvases were last drawn at
  x: number // content offset from the stage's top-left, in CSS pixels
  y: number
}

// Content smaller than the stage is centred; larger content can't be dragged
// so far that the stage shows a gap at either edge.
export function clampAxis(offset: number, content: number, stage: number): number {
  if (content <= stage) return (stage - content) / 2
  return Math.min(0, Math.max(stage - content, offset))
}

// Zooms to next, keeping the content point under (px, py) in place.
export function zoomAt(v: View, next: number, px: number, py: number): void {
  const z = Math.min(MAX_ZOOM, Math.max(1, next))
  v.x = px - ((px - v.x) * z) / v.zoom
  v.y = py - ((py - v.y) * z) / v.zoom
  v.zoom = z
}
