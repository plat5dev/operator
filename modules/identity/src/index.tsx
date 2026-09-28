import { Route, Routes } from "react-router-dom"
import { BaseProvider } from "./base"
import { MemberPage } from "./member"
import { OrgPage } from "./org"
import { OrgList } from "./orgs"
import { ServiceAccountPage } from "./service-account"
import { UserPage } from "./user"
import "./identity.css"

export default function Identity() {
  return (
    <BaseProvider>
      <Routes>
        <Route index element={<OrgList />} />
        <Route path="organizations/:organizationId" element={<OrgPage />} />
        <Route path="organizations/:organizationId/service-accounts/:serviceAccountId" element={<ServiceAccountPage />} />
        <Route path="members/:memberId" element={<MemberPage />} />
        <Route path="users/:userId" element={<UserPage />} />
      </Routes>
    </BaseProvider>
  )
}
