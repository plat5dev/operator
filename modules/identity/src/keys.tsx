import { useState } from "react"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Table from "@cloudscape-design/components/table"
import { failure, filled, seg, send, useList, when, type KeyRow } from "./api"
import { Empty, More, Secret } from "./ui"

export function Keys({ path }: { path: string }) {
  const keys = useList<KeyRow>(path, "keys")
  const [name, setName] = useState("")
  const [scopes, setScopes] = useState("")
  const [secret, setSecret] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create() {
    setBusy(true)
    setError("")
    setSecret("")
    const body: Record<string, unknown> = {}
    filled(body as Record<string, string>, "name", name)
    const labels = scopes.split(",").map((item) => item.trim()).filter(Boolean)
    if (labels.length) body.scopes = labels
    try {
      const res = await send("POST", path, body)
      const created = (await res.json()) as { key?: string }
      setSecret(created.key ?? "")
      setName("")
      setScopes("")
      keys.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(id: string) {
    setError("")
    try {
      await send("DELETE", `${path}/${seg(id)}`)
      keys.reload()
    } catch (err) {
      setError(failure(err))
    }
  }

  return (
    <SpaceBetween size="s">
      <Header variant="h2">API keys</Header>
      {secret ? <Secret label="Copy this key. It will not be shown again." value={secret} /> : null}
      {error || keys.error ? <Alert type="error">{error || keys.error}</Alert> : null}
      <Table
        items={keys.items}
        loading={keys.loading}
        trackBy="id"
        empty={<Empty />}
        columnDefinitions={[
          { id: "name", header: "Name", cell: (item) => item.name },
          { id: "prefix", header: "Prefix", cell: (item) => item.key_prefix },
          { id: "scopes", header: "Scopes", cell: (item) => (item.scopes ? item.scopes.join(", ") : "unrestricted") },
          { id: "created", header: "Created", cell: (item) => when(item.created_at) },
          { id: "revoked", header: "Revoked", cell: (item) => (item.revoked_at ? when(item.revoked_at) : "") },
          {
            id: "revoke",
            header: "",
            cell: (item) =>
              item.revoked_at ? null : (
                <Button onClick={() => void revoke(item.id)}>Revoke</Button>
              ),
          },
        ]}
      />
      <More hasMore={keys.hasMore} onMore={keys.loadMore} />
      <SpaceBetween size="s" direction="horizontal">
        <FormField label="Name">
          <Input value={name} onChange={({ detail }) => setName(detail.value)} />
        </FormField>
        <FormField label="Scopes" description="Comma-separated. Blank is unrestricted.">
          <Input value={scopes} onChange={({ detail }) => setScopes(detail.value)} />
        </FormField>
        <Button onClick={() => void create()} loading={busy}>
          Create key
        </Button>
      </SpaceBetween>
    </SpaceBetween>
  )
}
