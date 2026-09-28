import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import { failure, seg, send, useResource, type ServiceAccount } from "./api"
import { useBase } from "./base"
import { Load, Page } from "./ui"

export function ServiceAccountPage() {
  const { organizationId = "", serviceAccountId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const path = `/organizations/${seg(organizationId)}/service-accounts/${seg(serviceAccountId)}`
  const account = useResource<ServiceAccount>(path)
  const [name, setName] = useState("")
  const [error, setError] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!account.item) return
    setName(account.item.name)
  }, [account.item])

  async function save() {
    setBusy(true)
    setError("")
    try {
      await send("PATCH", path, { name: name.trim() })
      account.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  async function remove() {
    setBusy(true)
    setError("")
    try {
      await send("DELETE", path)
      navigate(`${base}/organizations/${seg(organizationId)}`)
    } catch (err) {
      setError(failure(err))
      setBusy(false)
    }
  }

  const item = account.item
  return (
    <Page>
      <Header variant="h1">
        <Link to={base}>Identity</Link>
      </Header>
      <Load loading={account.loading} error={account.error}>
        {item ? (
          <SpaceBetween size="l">
            <Header variant="h2">{item.name}</Header>
            {error ? <Alert type="error">{error}</Alert> : null}
            <div>Status {item.status}</div>
            <div>
              Member <Link to={`${base}/members/${seg(item.member_id)}`}>{item.member_id}</Link>
            </div>
            <div>Keys live on the member.</div>
            <SpaceBetween size="s" direction="horizontal">
              <FormField label="Name">
                <Input value={name} onChange={({ detail }) => setName(detail.value)} />
              </FormField>
              <Button onClick={() => void save()} loading={busy}>Save</Button>
            </SpaceBetween>
            {confirming ? (
              <SpaceBetween size="s" direction="horizontal">
                <Button onClick={() => void remove()} loading={busy}>Delete permanently</Button>
                <Button onClick={() => setConfirming(false)}>Cancel</Button>
              </SpaceBetween>
            ) : (
              <Button onClick={() => setConfirming(true)}>Delete service account</Button>
            )}
          </SpaceBetween>
        ) : null}
      </Load>
    </Page>
  )
}
