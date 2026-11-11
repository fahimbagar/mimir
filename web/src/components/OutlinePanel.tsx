import { useEffect, useMemo, useRef, useState } from 'react'
import { flattenOutline } from '../readers/outline'
import type { OutlineItem } from '../readers/types'

interface Props {
  items: OutlineItem[]
  activeId?: string
  showPages: boolean
  onPick: (item: OutlineItem) => void
  onClose: () => void
}

export function OutlinePanel({ items, activeId, showPages, onPick, onClose }: Props) {
  // Everything starts expanded; we track what the reader folded away.
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
  const listRef = useRef<HTMLUListElement>(null)

  const parents = useMemo(
    () => flattenOutline(items).filter((it) => it.children.length > 0).map((it) => it.id),
    [items],
  )

  // When the reading position moves into a folded section, unfold its
  // ancestors. Done during render (React's "adjust state on prop change"
  // pattern) so the list never paints with the active entry hidden.
  const [unfoldedFor, setUnfoldedFor] = useState(activeId)
  if (activeId !== unfoldedFor) {
    setUnfoldedFor(activeId)
    if (activeId) {
      const parts = activeId.split('.')
      const ancestors = parts.slice(0, -1).map((_, i) => parts.slice(0, i + 1).join('.'))
      if (ancestors.some((a) => collapsed.has(a))) {
        const next = new Set(collapsed)
        ancestors.forEach((a) => next.delete(a))
        setCollapsed(next)
      }
    }
  }

  useEffect(() => {
    if (!activeId) return
    const frame = requestAnimationFrame(() => {
      listRef.current?.querySelector('.is-active')?.scrollIntoView({ block: 'nearest' })
    })
    return () => cancelAnimationFrame(frame)
  }, [activeId])

  const toggle = (id: string) =>
    setCollapsed((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const renderNodes = (nodes: OutlineItem[], depth: number) =>
    nodes.map((item) => {
      const hasKids = item.children.length > 0
      const open = !collapsed.has(item.id)
      const reachable = item.page != null || item.href != null
      return (
        <li key={item.id}>
          <div
            className={`toc-row ${item.id === activeId ? 'is-active' : ''}`}
            style={{ paddingLeft: 6 + depth * 14 }}
          >
            {hasKids ? (
              <button
                className={`toc-twisty ${open ? 'is-open' : ''}`}
                aria-expanded={open}
                aria-label={open ? 'Collapse' : 'Expand'}
                onClick={() => toggle(item.id)}
              >
                <svg viewBox="0 0 24 24" aria-hidden>
                  <path d="M9 6l6 6-6 6" />
                </svg>
              </button>
            ) : (
              <span className="toc-twisty" aria-hidden />
            )}
            <button
              className="toc-link"
              disabled={!reachable}
              title={reachable ? item.label : `${item.label} (no target in this file)`}
              onClick={() => onPick(item)}
            >
              {item.label}
            </button>
            {showPages && item.page != null && <span className="toc-page">{item.page}</span>}
          </div>
          {hasKids && open && <ul>{renderNodes(item.children, depth + 1)}</ul>}
        </li>
      )
    })

  return (
    <aside className="toc" aria-label="Bookmarks">
      <div className="toc-head">
        <h2>Bookmarks</h2>
        {parents.length > 0 && (
          <div className="toc-head-actions">
            <button className="text-btn" onClick={() => setCollapsed(new Set())}>
              Expand all
            </button>
            <button className="text-btn" onClick={() => setCollapsed(new Set(parents))}>
              Collapse all
            </button>
          </div>
        )}
        <button className="icon-btn toc-close" aria-label="Close bookmarks" onClick={onClose}>
          <svg viewBox="0 0 24 24" aria-hidden>
            <path d="M6 6l12 12M18 6L6 18" />
          </svg>
        </button>
      </div>
      <ul className="toc-tree" ref={listRef}>
        {renderNodes(items, 0)}
      </ul>
    </aside>
  )
}
