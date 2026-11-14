import { useEffect, useState } from 'react'
import { getMe, setUnauthorizedHandler, signOut, type User } from './api'
import { Library } from './pages/Library'
import { Login } from './pages/Login'
import { Reader } from './pages/Reader'
import { navigate, usePath } from './router'

export function App() {
  const path = usePath()
  // undefined while checking the session, null when signed out.
  const [user, setUser] = useState<User | null | undefined>(undefined)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    getMe().then(setUser, (e: Error) => {
      setError(e.message)
      setUser(null)
    })
    setUnauthorizedHandler(() => setUser(null))
    return () => setUnauthorizedHandler(null)
  }, [])

  if (user === undefined) return null
  if (user === null) return <Login error={error} />

  const onSignOut = () => {
    signOut().then(
      () => {
        setUser(null)
        navigate('/')
      },
      (e: Error) => setError(e.message),
    )
  }

  const m = path.match(/^\/read\/([0-9a-f]+)\/?$/)
  return m ? <Reader key={m[1]} fileId={m[1]} /> : <Library user={user} onSignOut={onSignOut} />
}
