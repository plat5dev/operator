import { useCallback, useEffect, useState } from "react"

export async function errorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: { message?: string } }
    if (body.error?.message) return body.error.message
  } catch {
    // The body was not the error envelope.
  }
  return "Request failed."
}

export function failure(err: unknown): string {
  return err instanceof Error ? err.message : "Request failed."
}

export async function send(method: string, path: string, body?: unknown): Promise<Response> {
  const res = await fetch(path, {
    method,
    credentials: "include",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!res.ok) throw new Error(await errorMessage(res))
  return res
}

export async function get<T>(path: string): Promise<T> {
  const res = await send("GET", path)
  return (await res.json()) as T
}

export async function list<T extends { id: string }>(
  path: string,
  collection: string,
  startingAfter: string,
): Promise<{ items: T[]; hasMore: boolean }> {
  const url = new URL(path, "http://local")
  url.searchParams.set("limit", "50")
  if (startingAfter) url.searchParams.set("starting_after", startingAfter)
  const res = await send("GET", `${url.pathname}${url.search}`)
  const body = (await res.json()) as Record<string, unknown>
  const items = body[collection]
  if (!Array.isArray(items)) throw new Error("Request failed.")
  return { items: items as T[], hasMore: body.has_more === true }
}

export function useList<T extends { id: string }>(path: string, collection: string) {
  const [items, setItems] = useState<T[]>([])
  const [hasMore, setHasMore] = useState(false)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError("")
    list<T>(path, collection, "")
      .then((page) => {
        if (cancelled) return
        setItems(page.items)
        setHasMore(page.hasMore)
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(failure(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [path, collection, tick])

  const loadMore = useCallback(async () => {
    const last = items[items.length - 1]
    if (!last) return
    const page = await list<T>(path, collection, last.id)
    setItems((prev) => [...prev, ...page.items])
    setHasMore(page.hasMore)
  }, [items, path, collection])

  return { items, hasMore, error, loading, reload: () => setTick((n) => n + 1), loadMore }
}

export function useResource<T>(path: string) {
  const [item, setItem] = useState<T | null>(null)
  const [error, setError] = useState("")
  const [loading, setLoading] = useState(true)
  const [tick, setTick] = useState(0)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError("")
    get<T>(path)
      .then((next) => {
        if (!cancelled) setItem(next)
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setItem(null)
          setError(failure(err))
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [path, tick])

  return { item, error, loading, reload: () => setTick((n) => n + 1) }
}

export function seg(id: string): string {
  return encodeURIComponent(id)
}

export function filled(body: Record<string, string>, key: string, value: string): void {
  const trimmed = value.trim()
  if (trimmed) body[key] = trimmed
}

export function when(value: string): string {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export type Org = {
  id: string
  name: string
  slug: string
  created_at: string
  updated_at: string
}

export type Member = {
  id: string
  organization_id: string
  principal: string
  user_id: string | null
  service_account_id: string | null
  status: string
  added_by: string | null
  created_at: string
  updated_at: string
}

export type Invite = {
  id: string
  organization_id: string
  email: string | null
  token_prefix: string
  token?: string
  status: string
  max_uses: number | null
  use_count: number
  expires_at: string
  created_by: string | null
  created_at: string
}

export type ServiceAccount = {
  id: string
  organization_id: string
  member_id: string
  name: string
  status: string
  created_by_user_id: string | null
  created_at: string
  updated_at: string
}

export type Membership = {
  id: string
  organization: { id: string; name: string; slug: string }
  status: string
}

export type KeyRow = {
  id: string
  key_prefix: string
  name: string
  scopes: string[] | null
  created_at: string
  revoked_at: string | null
}
