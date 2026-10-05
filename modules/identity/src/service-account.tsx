import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { Alert, Breadcrumb, Button, Card, Descriptions, Flex, Form, Input, Typography } from "antd"
import { failure, seg, send, useResource, when, type ServiceAccount } from "./api"
import { useBase } from "./base"
import { confirmDanger, Id, Load, Page, PageHeader, Status } from "./ui"

export function ServiceAccountPage() {
  const { organizationId = "", serviceAccountId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const path = `/organizations/${seg(organizationId)}/service-accounts/${seg(serviceAccountId)}`
  const account = useResource<ServiceAccount>(path)
  const [form] = Form.useForm<{ name: string }>()
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!account.item) return
    form.setFieldsValue({ name: account.item.name })
  }, [account.item, form])

  async function save(values: { name: string }) {
    setBusy(true)
    setError("")
    try {
      await send("PATCH", path, { name: values.name.trim() })
      account.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setError("")
    try {
      await send("DELETE", path)
      navigate(`${base}/organizations/${seg(organizationId)}`)
    } catch (err) {
      setError(failure(err))
    }
  }

  const item = account.item
  return (
    <Page>
      <Load loading={account.loading} error={account.error}>
        {item ? (
          <>
            <Breadcrumb
              items={[
                { title: <Link to={base}>Identity</Link> },
                { title: <Link to={`${base}/organizations/${seg(organizationId)}`}>Organization</Link> },
                { title: item.name },
              ]}
            />
            <PageHeader title={item.name} description={<Id value={item.id} />} />
            {error ? <Alert type="error" showIcon title={error} /> : null}
            <Card>
              <Descriptions
                column={{ xs: 1, md: 2 }}
                items={[
                  { key: "status", label: "Status", children: <Status value={item.status} /> },
                  {
                    key: "member",
                    label: "Member",
                    children: <Link to={`${base}/members/${seg(item.member_id)}`}>{item.member_id}</Link>,
                  },
                  ...(item.created_by_user_id
                    ? [{ key: "created", label: "Created by", children: item.created_by_user_id }]
                    : []),
                  { key: "updated", label: "Updated", children: when(item.updated_at) },
                ]}
              />
              <Typography.Paragraph type="secondary" style={{ margin: "16px 0 0" }}>
                Keys live on the member.
              </Typography.Paragraph>
              <Form form={form} layout="vertical" onFinish={save} style={{ maxWidth: 480, marginTop: 8 }}>
                <Form.Item
                  label="Name"
                  name="name"
                  rules={[{ required: true, whitespace: true, message: "Enter a name." }]}
                >
                  <Input />
                </Form.Item>
                <Button type="primary" htmlType="submit" loading={busy}>
                  Save
                </Button>
              </Form>
              <Flex justify="flex-end" style={{ marginTop: 8 }}>
                <Button
                  danger
                  onClick={() => confirmDanger("Delete this service account?", "Delete permanently", remove)}
                >
                  Delete service account
                </Button>
              </Flex>
            </Card>
          </>
        ) : null}
      </Load>
    </Page>
  )
}
