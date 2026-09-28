import { useState } from "react"
import { Link, useNavigate } from "react-router-dom"
import Alert from "@cloudscape-design/components/alert"
import Button from "@cloudscape-design/components/button"
import Form from "@cloudscape-design/components/form"
import FormField from "@cloudscape-design/components/form-field"
import Header from "@cloudscape-design/components/header"
import Input from "@cloudscape-design/components/input"
import SpaceBetween from "@cloudscape-design/components/space-between"
import Table from "@cloudscape-design/components/table"
import { failure, filled, seg, send, useList, type Org } from "./api"
import { useBase } from "./base"
import { Empty, More, Page, onSubmit } from "./ui"

export function OrgList() {
  const base = useBase()
  const navigate = useNavigate()
  const orgs = useList<Org>("/organizations", "organizations")
  const [userId, setUserId] = useState("")
  const [name, setName] = useState("")
  const [slug, setSlug] = useState("")
  const [openUser, setOpenUser] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function create() {
    setBusy(true)
    setError("")
    const body: Record<string, string> = { name: name.trim() }
    filled(body, "slug", slug)
    try {
      const res = await send("POST", `/users/${seg(userId.trim())}/organizations`, body)
      const created = (await res.json()) as Org
      navigate(`${base}/organizations/${seg(created.id)}`)
    } catch (err) {
      setError(failure(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Page>
      <Header variant="h1" description="Every organization. There is no user directory.">
        Identity
      </Header>
      {error || orgs.error ? <Alert type="error">{error || orgs.error}</Alert> : null}
      <form
        onSubmit={onSubmit(async () => {
          const id = openUser.trim()
          if (id) navigate(`${base}/users/${seg(id)}`)
        })}
      >
        <SpaceBetween size="s" direction="horizontal">
          <FormField label="User ID">
            <Input value={openUser} onChange={({ detail }) => setOpenUser(detail.value)} />
          </FormField>
          <Button formAction="submit">Open user</Button>
        </SpaceBetween>
      </form>
      <Table
        header={<Header variant="h2">Organizations</Header>}
        items={orgs.items}
        loading={orgs.loading}
        trackBy="id"
        empty={<Empty />}
        columnDefinitions={[
          {
            id: "name",
            header: "Name",
            cell: (item) => <Link to={`${base}/organizations/${seg(item.id)}`}>{item.name}</Link>,
          },
          { id: "slug", header: "Slug", cell: (item) => item.slug },
          { id: "id", header: "ID", cell: (item) => item.id },
        ]}
      />
      <More hasMore={orgs.hasMore} onMore={orgs.loadMore} />
      <form onSubmit={onSubmit(create)}>
        <Form
          header={<Header variant="h2">Create organization</Header>}
          actions={<Button variant="primary" formAction="submit" loading={busy}>Create</Button>}
        >
          <SpaceBetween size="s">
            <FormField label="User ID" description="Customer user id. Creating an organization also adds this user as a member.">
              <Input value={userId} onChange={({ detail }) => setUserId(detail.value)} />
            </FormField>
            <FormField label="Name">
              <Input value={name} onChange={({ detail }) => setName(detail.value)} />
            </FormField>
            <FormField label="Slug" description="Optional. Derived from the name when blank.">
              <Input value={slug} onChange={({ detail }) => setSlug(detail.value)} />
            </FormField>
          </SpaceBetween>
        </Form>
      </form>
    </Page>
  )
}
