import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { Alert, Breadcrumb, Button, Card, Divider, Flex, Form, Input, Typography } from "antd"
import type { TableColumnsType } from "antd"
import {
  failure,
  filled,
  seg,
  send,
  useList,
  useResource,
  when,
  type Invite,
  type Member,
  type Org,
  type ServiceAccount,
} from "./api"
import { useBase } from "./base"
import { confirmDanger, DataTable, Id, Load, More, Page, PageHeader, Secret, Status } from "./ui"

export function OrgPage() {
  const { organizationId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const org = useResource<Org>(`/organizations/${seg(organizationId)}`)
  const [form] = Form.useForm<{ name: string; slug: string }>()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!org.item) return
    form.setFieldsValue({ name: org.item.name, slug: org.item.slug })
  }, [org.item, form])

  async function save(values: { name: string; slug: string }) {
    setBusy(true)
    setError("")
    try {
      await send("PATCH", `/organizations/${seg(organizationId)}`, {
        name: values.name.trim(),
        slug: values.slug.trim(),
      })
      org.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setError("")
    try {
      await send("DELETE", `/organizations/${seg(organizationId)}`)
      navigate(base)
    } catch (err) {
      setError(failure(err))
    }
  }

  return (
    <Page>
      <Load loading={org.loading} error={org.error}>
        {org.item ? (
          <>
            <Breadcrumb items={[{ title: <Link to={base}>Identity</Link> }, { title: org.item.name }]} />
            <PageHeader title={org.item.name} description={<Id value={org.item.id} />} />
            {error ? <Alert type="error" showIcon title={error} /> : null}
            <Card title="Details">
              <Form form={form} layout="vertical" onFinish={save} style={{ maxWidth: 480 }}>
                <Form.Item
                  label="Name"
                  name="name"
                  rules={[{ required: true, whitespace: true, message: "Enter a name." }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  label="Slug"
                  name="slug"
                  rules={[{ required: true, whitespace: true, message: "Enter a slug." }]}
                >
                  <Input />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={busy}>
                  Save
                </Button>
              </Form>
              <Flex justify="space-between" align="center" gap={16} wrap style={{ marginTop: 16 }}>
                <Typography.Text type="secondary">Updated {when(org.item.updated_at)}</Typography.Text>
                <Button
                  danger
                  onClick={() =>
                    confirmDanger(
                      "Delete this organization?",
                      "Delete permanently",
                      remove,
                      "Members, invites, service accounts, and keys are deleted with it.",
                    )
                  }
                >
                  Delete organization
                </Button>
              </Flex>
            </Card>
            <Members orgId={organizationId} />
            <Invites orgId={organizationId} />
            <Accounts orgId={organizationId} />
          </>
        ) : null}
      </Load>
    </Page>
  )
}

function Members({ orgId }: { orgId: string }) {
  const base = useBase()
  const members = useList<Member>(`/organizations/${seg(orgId)}/members`, "members")
  const [form] = Form.useForm<{ userId: string; addedBy?: string }>()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function add(values: { userId: string; addedBy?: string }) {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { user_id: values.userId.trim() }
    filled(body, "added_by", values.addedBy ?? "")
    try {
      await send("POST", `/organizations/${seg(orgId)}/members`, body)
      form.resetFields()
      members.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  const columns: TableColumnsType<Member> = [
    {
      title: "Member",
      dataIndex: "id",
      render: (id: string) => <Link to={`${base}/members/${seg(id)}`}>{id}</Link>,
    },
    { title: "Principal", dataIndex: "principal" },
    {
      title: "User",
      key: "who",
      render: (_, item) =>
        item.user_id ? (
          <Link to={`${base}/users/${seg(item.user_id)}`}>{item.user_id}</Link>
        ) : (
          (item.service_account_id ?? "")
        ),
    },
    { title: "Status", dataIndex: "status", render: (status: string) => <Status value={status} /> },
  ]

  return (
    <Card title="Members">
      {error || members.error ? (
        <Alert type="error" showIcon title={error || members.error} style={{ marginBottom: 16 }} />
      ) : null}
      <DataTable loading={members.loading} items={members.items} columns={columns} />
      <More hasMore={members.hasMore} onMore={members.loadMore} />
      <Divider />
      <Form form={form} layout="vertical" onFinish={add} style={{ maxWidth: 480 }}>
        <Form.Item
          label="User ID"
          name="userId"
          rules={[{ required: true, whitespace: true, message: "Enter a user id." }]}
        >
          <Input />
        </Form.Item>
        <Form.Item label="Added by" name="addedBy" extra="Customer user id, or blank.">
          <Input />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>
          Add member
        </Button>
      </Form>
    </Card>
  )
}

function Invites({ orgId }: { orgId: string }) {
  const invites = useList<Invite>(`/organizations/${seg(orgId)}/invites`, "invites")
  const [form] = Form.useForm<{ email?: string; createdBy?: string }>()
  const [token, setToken] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create(values: { email?: string; createdBy?: string }) {
    setBusy(true)
    setError("")
    setToken("")
    const body: Record<string, string> = {}
    filled(body, "email", values.email ?? "")
    filled(body, "created_by", values.createdBy ?? "")
    try {
      const res = await send("POST", `/organizations/${seg(orgId)}/invites`, body)
      const created = (await res.json()) as Invite
      setToken(created.token ?? "")
      form.resetFields()
      invites.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  async function revoke(id: string) {
    setError("")
    try {
      await send("DELETE", `/organizations/${seg(orgId)}/invites/${seg(id)}`)
      invites.reload()
    } catch (err) {
      setError(failure(err))
    }
  }

  const columns: TableColumnsType<Invite> = [
    { title: "Email", dataIndex: "email", render: (email: string | null) => email ?? "" },
    { title: "Status", dataIndex: "status", render: (status: string) => <Status value={status} /> },
    {
      title: "Token",
      key: "token",
      render: (_, item) =>
        item.token ? (
          <Typography.Text className="identity-id" copyable={{ text: item.token }}>
            {item.token}
          </Typography.Text>
        ) : (
          <span className="identity-id">{item.token_prefix}</span>
        ),
    },
    {
      title: "Uses",
      key: "uses",
      render: (_, item) => `${item.use_count}${item.max_uses == null ? "" : ` / ${item.max_uses}`}`,
    },
    { title: "Expires", dataIndex: "expires_at", render: (value: string) => when(value) },
    {
      title: "",
      key: "revoke",
      render: (_, item) =>
        item.status === "active" ? (
          <Button
            size="small"
            danger
            onClick={() => confirmDanger("Revoke this invite?", "Revoke", () => revoke(item.id))}
          >
            Revoke
          </Button>
        ) : null,
    },
  ]

  return (
    <Card title="Invites">
      {token ? (
        <div style={{ marginBottom: 16 }}>
          <Secret label="Copy this token. Active invites also list it." value={token} />
        </div>
      ) : null}
      {error || invites.error ? (
        <Alert type="error" showIcon title={error || invites.error} style={{ marginBottom: 16 }} />
      ) : null}
      <DataTable loading={invites.loading} items={invites.items} columns={columns} />
      <More hasMore={invites.hasMore} onMore={invites.loadMore} />
      <Divider />
      <Form form={form} layout="vertical" onFinish={create} style={{ maxWidth: 480 }}>
        <Form.Item label="Email" name="email" extra="Optional. Not mailed.">
          <Input type="email" />
        </Form.Item>
        <Form.Item label="Created by" name="createdBy" extra="Customer user id, or blank.">
          <Input />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>
          Create invite
        </Button>
      </Form>
    </Card>
  )
}

function Accounts({ orgId }: { orgId: string }) {
  const base = useBase()
  const accounts = useList<ServiceAccount>(`/organizations/${seg(orgId)}/service-accounts`, "service_accounts")
  const [form] = Form.useForm<{ name: string; createdBy?: string }>()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create(values: { name: string; createdBy?: string }) {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { name: values.name.trim() }
    filled(body, "created_by_user_id", values.createdBy ?? "")
    try {
      await send("POST", `/organizations/${seg(orgId)}/service-accounts`, body)
      form.resetFields()
      accounts.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  const columns: TableColumnsType<ServiceAccount> = [
    {
      title: "Name",
      dataIndex: "name",
      render: (name: string, item) => (
        <Link to={`${base}/organizations/${seg(orgId)}/service-accounts/${seg(item.id)}`}>{name}</Link>
      ),
    },
    { title: "Status", dataIndex: "status", render: (status: string) => <Status value={status} /> },
    {
      title: "Member",
      dataIndex: "member_id",
      render: (id: string) => <Link to={`${base}/members/${seg(id)}`}>{id}</Link>,
    },
  ]

  return (
    <Card title="Service accounts">
      {error || accounts.error ? (
        <Alert type="error" showIcon title={error || accounts.error} style={{ marginBottom: 16 }} />
      ) : null}
      <DataTable loading={accounts.loading} items={accounts.items} columns={columns} />
      <More hasMore={accounts.hasMore} onMore={accounts.loadMore} />
      <Divider />
      <Form form={form} layout="vertical" onFinish={create} style={{ maxWidth: 480 }}>
        <Form.Item label="Name" name="name" rules={[{ required: true, whitespace: true, message: "Enter a name." }]}>
          <Input />
        </Form.Item>
        <Form.Item label="Created by" name="createdBy" extra="Customer user id, or blank.">
          <Input />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>
          Create service account
        </Button>
      </Form>
    </Card>
  )
}
