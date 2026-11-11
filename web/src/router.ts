import { useSyncExternalStore } from 'react'

function subscribe(cb: () => void) {
  window.addEventListener('popstate', cb)
  return () => window.removeEventListener('popstate', cb)
}

export function usePath(): string {
  return useSyncExternalStore(subscribe, () => window.location.pathname)
}

export function navigate(to: string) {
  if (to === window.location.pathname) return
  history.pushState(null, '', to)
  window.dispatchEvent(new PopStateEvent('popstate'))
}
