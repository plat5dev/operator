import { useEffect, useState, type FormEvent } from "react"
import { Navigate, useSearchParams } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import Container from "@cloudscape-design/components/container"
import Form from "@cloudscape-design/components/form"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Spinner from "@cloudscape-design/components/spinner"
import { errorMessage, getSession, safeNext } from "./api"

export function LoginPage() {
  const [params] = useSearchParams()
  const next = safeNext(params.get("next"))
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
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

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      const res = await fetch("/login", {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ email, password }),
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
      <div className="shell-center">
        <Spinner size="large" />
      </div>
    )
  }

  return (
    <div className="login">
      <form onSubmit={onSubmit}>
        <Container header={<Header variant="h1">Operator</Header>}>
          <Form actions={<Button variant="primary" formAction="submit" loading={busy}>Log in</Button>}>
            <SpaceBetween size="l">
              {error ? <Alert type="error">{error}</Alert> : null}
              <FormField label="Email">
                <Input
                  type="email"
                  value={email}
                  autoComplete="username"
                  onChange={({ detail }) => setEmail(detail.value)}
                />
              </FormField>
              <FormField label="Password">
                <Input
                  type="password"
                  value={password}
                  autoComplete="current-password"
                  onChange={({ detail }) => setPassword(detail.value)}
                />
              </FormField>
            </SpaceBetween>
          </Form>
        </Container>
      </form>
    </div>
  )
}
