import { createContext, useContext, type ReactNode } from "react"
import { useLocation, useParams } from "react-router-dom"

const BaseContext = createContext("/")

export function BaseProvider({ children }: { children: ReactNode }) {
  const base = useModuleBase()
  return <BaseContext.Provider value={base}>{children}</BaseContext.Provider>
}

export function useBase(): string {
  return useContext(BaseContext)
}

function useModuleBase(): string {
  const { pathname } = useLocation()
  const splat = (useParams()["*"] ?? "").replace(/^\//, "")
  if (!splat || !pathname.endsWith(splat)) return pathname.replace(/\/$/, "") || "/"
  return pathname.slice(0, pathname.length - splat.length).replace(/\/$/, "") || "/"
}
