import type { FormEvent, ReactNode } from "react"
import Alert from "@cloudscape-design/components/alert"
import Box from "@cloudscape-design/components/box"
import Button from "@cloudscape-design/components/button"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Spinner from "@cloudscape-design/components/spinner"

export function Page({ children }: { children: ReactNode }) {
  return (
    <div className="identity-page">
      <SpaceBetween size="l">{children}</SpaceBetween>
    </div>
  )
}

export function Load({ loading, error, children }: { loading: boolean; error: string; children: ReactNode }) {
  if (loading) return <Spinner />
  if (error) return <Alert type="error">{error}</Alert>
  return children
}

export function More({ hasMore, onMore }: { hasMore: boolean; onMore: () => Promise<void> }) {
  if (!hasMore) return null
  return <Button onClick={() => void onMore()}>Load more</Button>
}

export function Secret({ label, value }: { label: string; value: string }) {
  return (
    <Alert type="info">
      <SpaceBetween size="xs">
        <span>{label}</span>
        <span className="identity-secret">{value}</span>
      </SpaceBetween>
    </Alert>
  )
}

export function Empty() {
  return <Box color="text-body-secondary">None.</Box>
}

export function onSubmit(fn: () => Promise<void>) {
  return (event: FormEvent) => {
    event.preventDefault()
    void fn()
  }
}
