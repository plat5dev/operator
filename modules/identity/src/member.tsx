import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import Header from "@cloudscape-design/components/header"
import SpaceBetween from "@cloudscape-design/components/space-between"
import { failure, seg, send, useResource, when, type Member } from "./api"
import { useBase } from "./base"
import { Keys } from "./keys"
import { Load, Page } from "./ui"

export function MemberPage() {
  const { memberId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const member = useResource<Member>(`/members/${seg(memberId)}`)
  const [error, setError] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  async function setStatus(status: string) {
    setBusy(true)
    setError("")
    try {
      await send("PATCH", `/members/${seg(memberId)}`, { status })
      member.reload()
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
      await send("DELETE", `/members/${seg(memberId)}`)
      if (member.item) navigate(`${base}/organizations/${seg(member.item.organization_id)}`)
    } catch (err) {
      setError(failure(err))
      setBusy(false)
    }
  }

  const item = member.item
  return (
    <Page>
      <Header variant="h1">
        <Link to={base}>Identity</Link>
      </Header>
      <Load loading={member.loading} error={member.error}>
        {item ? (
          <SpaceBetween size="l">
            <Header variant="h2">{item.id}</Header>
            {error ? <Alert type="error">{error}</Alert> : null}
            <SpaceBetween size="xs">
              <div>
                Organization <Link to={`${base}/organizations/${seg(item.organization_id)}`}>{item.organization_id}</Link>
              </div>
              <div>Principal {item.principal}</div>
              <div>Status {item.status}</div>
              {item.user_id ? (
                <div>
                  User <Link to={`${base}/users/${seg(item.user_id)}`}>{item.user_id}</Link>
                </div>
              ) : null}
              {item.service_account_id ? (
                <div>
                  Service account{" "}
                  <Link to={`${base}/organizations/${seg(item.organization_id)}/service-accounts/${seg(item.service_account_id)}`}>
                    {item.service_account_id}
                  </Link>
                </div>
              ) : null}
              {item.added_by ? <div>Added by {item.added_by}</div> : null}
              <div>Updated {when(item.updated_at)}</div>
            </SpaceBetween>
            <SpaceBetween size="s" direction="horizontal">
              {item.status === "active" ? (
                <Button onClick={() => void setStatus("suspended")} loading={busy}>Suspend</Button>
              ) : (
                <Button onClick={() => void setStatus("active")} loading={busy}>Activate</Button>
              )}
              {confirming ? (
                <Button onClick={() => void remove()} loading={busy}>Remove permanently</Button>
              ) : (
                <Button onClick={() => setConfirming(true)}>Remove</Button>
              )}
            </SpaceBetween>
            <Keys path={`/members/${seg(item.id)}/api-keys`} />
          </SpaceBetween>
        ) : null}
      </Load>
    </Page>
  )
}
