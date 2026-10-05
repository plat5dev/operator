import { useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import { Alert, Button, Card, Flex, Form, Input } from "antd"
import type { TableColumnsType } from "antd"
import { failure, filled, seg, send, useList, type Org } from "./api"
import { useBase } from "./base"
import { DataTable, Id, More, Page, PageHeader } from "./ui"

export function OrgList() {
  const base = useBase()
  const navigate = useNavigate()
  const orgs = useList<Org>("/organizations", "organizations")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create(values: { userId: string; name: string; slug?: string }) {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { name: values.name.trim() }
    filled(body, "slug", values.slug ?? "")
    try {
      const res = await send("POST", `/users/${seg(values.userId.trim())}/organizations`, body)
      const created = (await res.json()) as Org
      navigate(`${base}/organizations/${seg(created.id)}`)
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  const columns: TableColumnsType<Org> = [
    {
      title: "Name",
      dataIndex: "name",
      render: (name: string, item) => <Link to={`${base}/organizations/${seg(item.id)}`}>{name}</Link>,
    },
    { title: "Slug", dataIndex: "slug" },
    { title: "ID", dataIndex: "id", render: (id: string) => <Id value={id} /> },
  ]

  return (
    <Page>
      <PageHeader title="Identity" description="Every organization. There is no user directory." />
      {error || orgs.error ? <Alert type="error" showIcon title={error || orgs.error} /> : null}
      <Card title="Open user">
        <Form
          layout="vertical"
          onFinish={(values: { userId: string }) => {
            navigate(`${base}/users/${seg(values.userId.trim())}`)
          }}
        >
          <Flex gap={12} align="flex-end" wrap>
            <Form.Item
              label="User ID"
              name="userId"
              rules={[{ required: true, whitespace: true, message: "Enter a user id." }]}
              style={{ marginBottom: 0, flex: "1 1 280px" }}
            >
              <Input />
            </Form.Item>
            <Form.Item style={{ marginBottom: 0 }}>
              <Button htmlType="submit">Open user</Button>
            </Form.Item>
          </Flex>
        </Form>
      </Card>
      <Card title="Organizations">
        <DataTable loading={orgs.loading} items={orgs.items} columns={columns} />
        <More hasMore={orgs.hasMore} onMore={orgs.loadMore} />
      </Card>
      <Card title="Create organization">
        <Form layout="vertical" onFinish={create} style={{ maxWidth: 480 }}>
          <Form.Item
            label="User ID"
            name="userId"
            extra="Customer user id. Creating an organization also adds this user as a member."
            rules={[{ required: true, whitespace: true, message: "Enter a user id." }]}
          >
            <Input />
          </Form.Item>
          <Form.Item label="Name" name="name" rules={[{ required: true, whitespace: true, message: "Enter a name." }]}>
            <Input />
          </Form.Item>
          <Form.Item label="Slug" name="slug" extra="Optional. Derived from the name when blank.">
            <Input />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy}>
            Create
          </Button>
        </Form>
      </Card>
    </Page>
  )
}
