import { useState } from "react"
import { Alert, Button, Card, Divider, Form, Input } from "antd"
import type { TableColumnsType } from "antd"
import { failure, filled, seg, send, useList, when, type KeyRow } from "./api"
import { confirmDanger, DataTable, More, Secret } from "./ui"

export function Keys({ path }: { path: string }) {
  const keys = useList<KeyRow>(path, "keys")
  const [form] = Form.useForm<{ name?: string; scopes?: string }>()
  const [secret, setSecret] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create(values: { name?: string; scopes?: string }) {
    setBusy(true)
    setError("")
    setSecret("")
    const body: Record<string, unknown> = {}
    filled(body as Record<string, string>, "name", values.name ?? "")
    const labels = (values.scopes ?? "")
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean)
    if (labels.length) body.scopes = labels
    try {
      const res = await send("POST", path, body)
      const created = (await res.json()) as { key?: string }
      setSecret(created.key ?? "")
      form.resetFields()
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

  const columns: TableColumnsType<KeyRow> = [
    { title: "Name", dataIndex: "name" },
    { title: "Prefix", dataIndex: "key_prefix", render: (prefix: string) => <span className="identity-id">{prefix}</span> },
    {
      title: "Scopes",
      dataIndex: "scopes",
      render: (scopes: string[] | null) => (scopes ? scopes.join(", ") : "unrestricted"),
    },
    { title: "Created", dataIndex: "created_at", render: (value: string) => when(value) },
    {
      title: "Revoked",
      dataIndex: "revoked_at",
      render: (value: string | null) => (value ? when(value) : ""),
    },
    {
      title: "",
      key: "revoke",
      render: (_, item) =>
        item.revoked_at ? null : (
          <Button size="small" danger onClick={() => confirmDanger("Revoke this key?", "Revoke", () => revoke(item.id))}>
            Revoke
          </Button>
        ),
    },
  ]

  return (
    <Card title="API keys">
      {secret ? (
        <div style={{ marginBottom: 16 }}>
          <Secret label="Copy this key. It will not be shown again." value={secret} />
        </div>
      ) : null}
      {error || keys.error ? (
        <Alert type="error" showIcon title={error || keys.error} style={{ marginBottom: 16 }} />
      ) : null}
      <DataTable loading={keys.loading} items={keys.items} columns={columns} />
      <More hasMore={keys.hasMore} onMore={keys.loadMore} />
      <Divider />
      <Form form={form} layout="vertical" onFinish={create} style={{ maxWidth: 480 }}>
        <Form.Item label="Name" name="name">
          <Input />
        </Form.Item>
        <Form.Item label="Scopes" name="scopes" extra="Comma-separated. Blank is unrestricted.">
          <Input />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>
          Create key
        </Button>
      </Form>
    </Card>
  )
}
