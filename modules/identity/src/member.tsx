import { useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import { Alert, Breadcrumb, Button, Card, Descriptions, Flex } from "antd"
import { failure, seg, send, useResource, when, type Member } from "./api"
import { useBase } from "./base"
import { Keys } from "./keys"
import { confirmDanger, Id, Load, Page, PageHeader, Status } from "./ui"

export function MemberPage() {
  const { memberId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const member = useResource<Member>(`/members/${seg(memberId)}`)
  const [error, setError] = useState("")
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
    setError("")
    try {
      await send("DELETE", `/members/${seg(memberId)}`)
      if (member.item) navigate(`${base}/organizations/${seg(member.item.organization_id)}`)
    } catch (err) {
      setError(failure(err))
    }
  }

  const item = member.item
  return (
    <Page>
      <Load loading={member.loading} error={member.error}>
        {item ? (
          <>
            <Breadcrumb
              items={[
                { title: <Link to={base}>Identity</Link> },
                { title: <Link to={`${base}/organizations/${seg(item.organization_id)}`}>Organization</Link> },
                { title: "Member" },
              ]}
            />
            <PageHeader
              title="Member"
              description={<Id value={item.id} />}
              extra={
                <Flex gap={8} wrap>
                  {item.status === "active" ? (
                    <Button loading={busy} onClick={() => void setStatus("suspended")}>
                      Suspend
                    </Button>
                  ) : (
                    <Button loading={busy} onClick={() => void setStatus("active")}>
                      Activate
                    </Button>
                  )}
                  <Button
                    danger
                    onClick={() => confirmDanger("Remove this member?", "Remove permanently", remove)}
                  >
                    Remove
                  </Button>
                </Flex>
              }
            />
            {error ? <Alert type="error" showIcon title={error} /> : null}
            <Card>
              <Descriptions
                column={{ xs: 1, md: 2 }}
                items={[
                  {
                    key: "org",
                    label: "Organization",
                    children: (
                      <Link to={`${base}/organizations/${seg(item.organization_id)}`}>{item.organization_id}</Link>
                    ),
                  },
                  { key: "principal", label: "Principal", children: item.principal },
                  { key: "status", label: "Status", children: <Status value={item.status} /> },
                  ...(item.user_id
                    ? [
                        {
                          key: "user",
                          label: "User",
                          children: <Link to={`${base}/users/${seg(item.user_id)}`}>{item.user_id}</Link>,
                        },
                      ]
                    : []),
                  ...(item.service_account_id
                    ? [
                        {
                          key: "sa",
                          label: "Service account",
                          children: (
                            <Link
                              to={`${base}/organizations/${seg(item.organization_id)}/service-accounts/${seg(item.service_account_id)}`}
                            >
                              {item.service_account_id}
                            </Link>
                          ),
                        },
                      ]
                    : []),
                  ...(item.added_by ? [{ key: "added", label: "Added by", children: item.added_by }] : []),
                  { key: "updated", label: "Updated", children: when(item.updated_at) },
                ]}
              />
            </Card>
            <Keys path={`/members/${seg(item.id)}/api-keys`} />
          </>
        ) : null}
      </Load>
    </Page>
  )
}
