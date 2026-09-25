export type Session = {
  id: string
  email: string
}

export type Service = {
  id: string
  title: string
  basePath: string
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

export function readServices(): Service[] {
  const text = document.getElementById("operator-services")?.textContent?.trim() ?? ""
  if (!text || text === "__SERVICES__") return []
  try {
    const parsed: unknown = JSON.parse(text)
    if (!Array.isArray(parsed)) return []
    return parsed.flatMap((item) => {
      if (!item || typeof item !== "object") return []
      const row = item as Record<string, unknown>
      if (typeof row.id !== "string" || typeof row.title !== "string" || typeof row.basePath !== "string") return []
      if (!row.basePath.startsWith("/") || row.basePath.startsWith("//")) return []
      return [{ id: row.id, title: row.title, basePath: row.basePath }]
    })
  } catch {
    return []
  }
}

export function safeNext(raw: string | null): string {
  if (!raw || !raw.startsWith("/") || raw.startsWith("//") || raw.startsWith("/\\")) return "/"
  return raw
}
