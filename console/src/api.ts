export type Session = {
  id: string
  email: string
}

export type Module = {
  id: string
  title: string
  basePath: string
  entry: string
}

export async function errorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { message?: string } }
    if (body.error?.message) return body.error.message
  } catch {
    // The body was not the error envelope.
  }
  return "Request failed."
}

export async function getSession(): Promise<Session | null> {
  const res = await fetch("/session", { credentials: "include" })
  if (res.status === 401) return null
  if (!res.ok) throw new Error(await errorMessage(res))
  return (await res.json()) as Session
}

export function readModules(): Module[] {
  const text = document.getElementById("operator-modules")?.textContent?.trim() ?? ""
  if (!text || text === "__MODULES__") return []
  try {
    const parsed: unknown = JSON.parse(text)
    if (!Array.isArray(parsed)) return []
    return parsed.flatMap((item) => {
      if (!item || typeof item !== "object") return []
      const row = item as Record<string, unknown>
      if (typeof row.id !== "string" || typeof row.title !== "string" || typeof row.basePath !== "string" || typeof row.entry !== "string") return []
      if (!row.basePath.startsWith("/") || row.basePath.startsWith("//")) return []
      if (!row.entry.startsWith("/modules/") || row.entry.includes("..") || row.entry.includes("\\") || row.entry.includes("?") || row.entry.includes("#")) return []
      return [{ id: row.id, title: row.title, basePath: row.basePath, entry: row.entry }]
    })
  } catch {
    return []
  }
}

export function safeNext(raw: string | null): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/"
  return raw
}
