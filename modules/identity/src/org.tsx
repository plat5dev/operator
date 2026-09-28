import { useEffect, useState } from "react"
import { Link, useNavigate, useParams } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Table from "@cloudscape-design/components/table"
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
import { Empty, Load, More, Page, Secret } from "./ui"

export function OrgPage() {
  const { organizationId = "" } = useParams()
  const base = useBase()
  const navigate = useNavigate()
  const org = useResource<Org>(`/organizations/${seg(organizationId)}`)
  const [name, setName] = useState("")
  const [slug, setSlug] = useState("")
  const [error, setError] = useState("")
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!org.item) return
    setName(org.item.name)
    setSlug(org.item.slug)
  }, [org.item])

  async function save() {
    setBusy(true)
    setError("")
    try {
      await send("PATCH", `/organizations/${seg(organizationId)}`, { name: name.trim(), slug: slug.trim() })
      org.reload()
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
      await send("DELETE", `/organizations/${seg(organizationId)}`)
      navigate(base)
    } catch (err) {
      setError(failure(err))
      setBusy(false)
    }
  }

  return (
    <Page>
      <Header variant="h1">
        <Link to={base}>Identity</Link>
      </Header>
      <Load loading={org.loading} error={org.error}>
        {org.item ? (
          <SpaceBetween size="l">
            <Header variant="h2">{org.item.name}</Header>
            {error ? <Alert type="error">{error}</Alert> : null}
            <SpaceBetween size="s" direction="horizontal">
              <FormField label="Name">
                <Input value={name} onChange={({ detail }) => setName(detail.value)} />
              </FormField>
              <FormField label="Slug">
                <Input value={slug} onChange={({ detail }) => setSlug(detail.value)} />
              </FormField>
              <Button onClick={() => void save()} loading={busy}>Save</Button>
            </SpaceBetween>
            {confirming ? (
              <SpaceBetween size="s" direction="horizontal">
                <Button onClick={() => void remove()} loading={busy}>Delete permanently</Button>
                <Button onClick={() => setConfirming(false)}>Cancel</Button>
              </SpaceBetween>
            ) : (
              <Button onClick={() => setConfirming(true)}>Delete organization</Button>
            )}
            <Members orgId={organizationId} />
            <Invites orgId={organizationId} />
            <Accounts orgId={organizationId} />
          </SpaceBetween>
        ) : null}
      </Load>
    </Page>
  )
}

function Members({ orgId }: { orgId: string }) {
  const base = useBase()
  const members = useList<Member>(`/organizations/${seg(orgId)}/members`, "members")
  const [userId, setUserId] = useState("")
  const [addedBy, setAddedBy] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function add() {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { user_id: userId.trim() }
    filled(body, "added_by", addedBy)
    try {
      await send("POST", `/organizations/${seg(orgId)}/members`, body)
      setUserId("")
      setAddedBy("")
      members.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SpaceBetween size="s">
      <Header variant="h2">Members</Header>
      {error || members.error ? <Alert type="error">{error || members.error}</Alert> : null}
      <Table
        items={members.items}
        loading={members.loading}
        trackBy="id"
        empty={<Empty />}
        columnDefinitions={[
          {
            id: "id",
            header: "Member",
            cell: (item) => <Link to={`${base}/members/${seg(item.id)}`}>{item.id}</Link>,
          },
          { id: "principal", header: "Principal", cell: (item) => item.principal },
          {
            id: "who",
            header: "User",
            cell: (item) =>
              item.user_id ? <Link to={`${base}/users/${seg(item.user_id)}`}>{item.user_id}</Link> : item.service_account_id ?? "",
          },
          { id: "status", header: "Status", cell: (item) => item.status },
        ]}
      />
      <More hasMore={members.hasMore} onMore={members.loadMore} />
      <SpaceBetween size="s" direction="horizontal">
        <FormField label="User ID">
          <Input value={userId} onChange={({ detail }) => setUserId(detail.value)} />
        </FormField>
        <FormField label="Added by" description="Customer user id, or blank.">
          <Input value={addedBy} onChange={({ detail }) => setAddedBy(detail.value)} />
        </FormField>
        <Button onClick={() => void add()} loading={busy}>Add member</Button>
      </SpaceBetween>
    </SpaceBetween>
  )
}

function Invites({ orgId }: { orgId: string }) {
  const invites = useList<Invite>(`/organizations/${seg(orgId)}/invites`, "invites")
  const [email, setEmail] = useState("")
  const [createdBy, setCreatedBy] = useState("")
  const [token, setToken] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create() {
    setBusy(true)
    setError("")
    setToken("")
    const body: Record<string, string> = {}
    filled(body, "email", email)
    filled(body, "created_by", createdBy)
    try {
      const res = await send("POST", `/organizations/${seg(orgId)}/invites`, body)
      const created = (await res.json()) as Invite
      setToken(created.token ?? "")
      setEmail("")
      setCreatedBy("")
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

  return (
    <SpaceBetween size="s">
      <Header variant="h2">Invites</Header>
      {token ? <Secret label="Copy this token. Active invites also list it." value={token} /> : null}
      {error || invites.error ? <Alert type="error">{error || invites.error}</Alert> : null}
      <Table
        items={invites.items}
        loading={invites.loading}
        trackBy="id"
        empty={<Empty />}
        columnDefinitions={[
          { id: "email", header: "Email", cell: (item) => item.email ?? "" },
          { id: "status", header: "Status", cell: (item) => item.status },
          { id: "token", header: "Token", cell: (item) => item.token ?? item.token_prefix },
          { id: "uses", header: "Uses", cell: (item) => `${item.use_count}${item.max_uses == null ? "" : ` / ${item.max_uses}`}` },
          { id: "expires", header: "Expires", cell: (item) => when(item.expires_at) },
          {
            id: "revoke",
            header: "",
            cell: (item) =>
              item.status === "active" ? <Button onClick={() => void revoke(item.id)}>Revoke</Button> : null,
          },
        ]}
      />
      <More hasMore={invites.hasMore} onMore={invites.loadMore} />
      <SpaceBetween size="s" direction="horizontal">
        <FormField label="Email" description="Optional. Not mailed.">
          <Input value={email} onChange={({ detail }) => setEmail(detail.value)} />
        </FormField>
        <FormField label="Created by" description="Customer user id, or blank.">
          <Input value={createdBy} onChange={({ detail }) => setCreatedBy(detail.value)} />
        </FormField>
        <Button onClick={() => void create()} loading={busy}>Create invite</Button>
      </SpaceBetween>
    </SpaceBetween>
  )
}

function Accounts({ orgId }: { orgId: string }) {
  const base = useBase()
  const accounts = useList<ServiceAccount>(`/organizations/${seg(orgId)}/service-accounts`, "service_accounts")
  const [name, setName] = useState("")
  const [createdBy, setCreatedBy] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create() {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { name: name.trim() }
    filled(body, "created_by_user_id", createdBy)
    try {
      await send("POST", `/organizations/${seg(orgId)}/service-accounts`, body)
      setName("")
      setCreatedBy("")
      accounts.reload()
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <SpaceBetween size="s">
      <Header variant="h2">Service accounts</Header>
      {error || accounts.error ? <Alert type="error">{error || accounts.error}</Alert> : null}
      <Table
        items={accounts.items}
        loading={accounts.loading}
        trackBy="id"
        empty={<Empty />}
        columnDefinitions={[
          {
            id: "name",
            header: "Name",
            cell: (item) => (
              <Link to={`${base}/organizations/${seg(orgId)}/service-accounts/${seg(item.id)}`}>{item.name}</Link>
            ),
          },
          { id: "status", header: "Status", cell: (item) => item.status },
          {
            id: "member",
            header: "Member",
            cell: (item) => <Link to={`${base}/members/${seg(item.member_id)}`}>{item.member_id}</Link>,
          },
        ]}
      />
      <More hasMore={accounts.hasMore} onMore={accounts.loadMore} />
      <SpaceBetween size="s" direction="horizontal">
        <FormField label="Name">
          <Input value={name} onChange={({ detail }) => setName(detail.value)} />
        </FormField>
        <FormField label="Created by" description="Customer user id, or blank.">
          <Input value={createdBy} onChange={({ detail }) => setCreatedBy(detail.value)} />
        </FormField>
        <Button onClick={() => void create()} loading={busy}>Create service account</Button>
      </SpaceBetween>
    </SpaceBetween>
  )
}
