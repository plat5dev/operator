import { createContext, useContext, useEffect, useState, type ReactNode } from "react"
import { Navigate, useLocation } from "react-router-dom"
import Spinner from "@cloudscape-design/components/spinner"
import { getSession, safeNext, type Session } from "./api"

const SessionContext = createContext<Session | null>(null)

export function useSession(): Session {
  const session = useContext(SessionContext)
  if (!session) throw new Error("session missing")
  return session
}

export function RequireSession({ children }: { children: ReactNode }) {
  const location = useLocation()
  const [session, setSession] = useState<Session | null | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    getSession()
      .then((next) => {
        if (!cancelled) setSession(next)
      })
      .catch(() => {
        if (!cancelled) setSession(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (session === undefined) {
    return (
      <div className="shell-center">
        <Spinner size="large" />
      </div>
    )
  }
  if (session === null) {
    const next = safeNext(location.pathname + location.search)
    return <Navigate to={next === "/" ? "/login" : `/login?next=${encodeURIComponent(next)}`} replace />
  }
  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>
}
