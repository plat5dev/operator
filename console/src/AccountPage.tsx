import { useState } from "react"
import { Alert, Button, Card, Form, Input, Typography } from "antd"
import { errorMessage } from "./api"
import { useSession } from "./session"

type PasswordValues = {
  current: string
  password: string
  confirm: string
}

export function AccountPage() {
  const session = useSession()
  const [form] = Form.useForm<PasswordValues>()
  const [error, setError] = useState("")
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  async function onSubmit(values: PasswordValues) {
    setSaved(false)
    setBusy(true)
    setError("")
    try {
      const res = await fetch("/account/password", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ current_password: values.current, password: values.password }),
      })
      if (!res.ok) {
        setError(await errorMessage(res))
        return
      }
      form.resetFields()
      setSaved(true)
    } catch {
      setError("Request failed.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <Card title="Account settings" style={{ maxWidth: 480 }}>
      {error ? <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} /> : null}
      {saved ? <Alert type="success" showIcon title="Password changed." style={{ marginBottom: 16 }} /> : null}
      <Form form={form} layout="vertical" onFinish={onSubmit}>
        <Form.Item label="Email">
          <Typography.Text>{session.email}</Typography.Text>
        </Form.Item>
        <Form.Item
          label="Current password"
          name="current"
          rules={[{ required: true, message: "Enter your current password." }]}
        >
          <Input.Password autoComplete="current-password" />
        </Form.Item>
        <Form.Item label="New password" name="password" rules={[{ required: true, message: "Enter a new password." }]}>
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Form.Item
          label="Confirm new password"
          name="confirm"
          dependencies={["password"]}
          rules={[
            { required: true, message: "Confirm the new password." },
            ({ getFieldValue }) => ({
              validator(_, value) {
                if (!value || getFieldValue("password") === value) return Promise.resolve()
                return Promise.reject(new Error("Passwords do not match."))
              },
            }),
          ]}
        >
          <Input.Password autoComplete="new-password" />
        </Form.Item>
        <Button type="primary" htmlType="submit" loading={busy}>
          Change password
        </Button>
      </Form>
    </Card>
  )
}
