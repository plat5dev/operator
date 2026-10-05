import { useEffect, useState } from "react"
import { Navigate, useSearchParams } from "react-router-dom"
import { Alert, Button, Card, Form, Input, Spin, Typography } from "antd"
import { errorMessage, getSession, safeNext } from "./api"

type LoginValues = {
  email: string
  password: string
}

export function LoginPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get("next"))
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [authed, setAuthed] = useState<boolean | undefined>(undefined)

  useEffect(() => {
    let cancelled = false
    getSession()
      .then((session) => {
        if (!cancelled) setAuthed(session !== null)
      })
      .catch(() => {
        if (!cancelled) setAuthed(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function onSubmit(values: LoginValues) {
    setBusy(true)
    setError("")
    try {
      const res = await fetch("/login", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(values),
      })
      if (!res.ok) {
        setError(await errorMessage(res))
        return
      }
      setAuthed(true)
    } catch {
      setError("Request failed.")
    } finally {
      setBusy(false)
    }
  }

  if (authed) return <Navigate to={next} replace />
  if (authed === undefined) {
    return (
      <div className="session-pending">
        <Spin size="large" />
      </div>
    )
  }

  return (
    <div className="login">
      <Card className="login-card" variant="outlined">
        <Typography.Title level={3} style={{ marginTop: 0 }}>
          Operator
        </Typography.Title>
        {error ? <Alert type="error" showIcon title={error} style={{ marginBottom: 16 }} /> : null}
        <Form layout="vertical" onFinish={onSubmit}>
          <Form.Item label="Email" name="email" rules={[{ required: true, message: "Enter an email." }]}>
            <Input type="email" autoComplete="username" size="large" />
          </Form.Item>
          <Form.Item label="Password" name="password" rules={[{ required: true, message: "Enter a password." }]}>
            <Input.Password autoComplete="current-password" size="large" />
          </Form.Item>
          <Button type="primary" htmlType="submit" loading={busy} block size="large">
            Log in
          </Button>
        </Form>
      </Card>
    </div>
  )
}
