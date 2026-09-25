import { useState, type FormEvent } from "react"
import Alert from "@cloudscape-design/components/alert"
import Box from "@cloudscape-design/components/box"
import Button from "@cloudscape-design/components/button"
import Container from "@cloudscape-design/components/container"
import Form from "@cloudscape-design/components/form"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import { errorMessage } from "./api"
import { useSession } from "./session"

export function AccountPage() {
  const session = useSession()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState("")
  const [saved, setSaved] = useState(false)
  const [busy, setBusy] = useState(false)

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setSaved(false)
    if (next !== confirm) {
      setError("Passwords do not match.")
      return
    }
    setBusy(true)
    setError("")
    try {
      const res = await fetch("/account/password", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ current_password: current, password: next }),
      })
      if (!res.ok) {
        setError(await errorMessage(res))
        return
      }
      setCurrent("")
      setNext("")
      setConfirm("")
      setSaved(true)
    } catch {
      setError("Request failed.")
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="shell-page">
      <form onSubmit={onSubmit}>
        <Form
          header={<Header variant="h1">Account settings</Header>}
          actions={<Button variant="primary" formAction="submit" loading={busy}>Change password</Button>}
        >
          <Container>
            <SpaceBetween size="l">
              {error ? <Alert type="error">{error}</Alert> : null}
              {saved ? <Alert type="success">Password changed.</Alert> : null}
              <FormField label="Email">
                <Box>{session.email}</Box>
              </FormField>
              <FormField label="Current password">
                <Input
                  type="password"
                  value={current}
                  autoComplete="current-password"
                  onChange={({ detail }) => setCurrent(detail.value)}
                />
              </FormField>
              <FormField label="New password">
                <Input
                  type="password"
                  value={next}
                  autoComplete="new-password"
                  onChange={({ detail }) => setNext(detail.value)}
                />
              </FormField>
              <FormField label="Confirm new password">
                <Input
                  type="password"
                  value={confirm}
                  autoComplete="new-password"
                  onChange={({ detail }) => setConfirm(detail.value)}
                />
              </FormField>
            </SpaceBetween>
          </Container>
        </Form>
      </form>
    </div>
  )
}
