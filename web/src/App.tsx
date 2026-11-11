import { Library } from './pages/Library'
import { Reader } from './pages/Reader'
import { usePath } from './router'

export function App() {
  const path = usePath()
  const m = path.match(/^\/read\/([0-9a-f]+)\/?$/)
  return m ? <Reader key={m[1]} fileId={m[1]} /> : <Library />
}
