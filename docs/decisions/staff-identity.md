# Staff identity

Why operators are whoever the staff IdP vouches for, and why nothing in this gateway narrows what they may call. The rules: [`reference/idp.md`](../reference/idp.md), [`concepts/model.md`](../concepts/model.md#principles).

| Decision | Why |
|----------|-----|
| One staff IdP, never the customer IdP | A customer account must never become a staff account through a claim or a config slip |
| The only credential is a bearer JWT; no cookies, sessions or API keys | Session state would make this gateway an account store. Keys are deferred until a staff caller can't get a token ([`TODO.md`](../../TODO.md)) |
| An audience is required | A staff IdP issues tokens for many internal apps. Without the audience check, any of them would be an operator token |
| Adding or removing staff happens in the IdP, by assigning the application | One place to offboard someone |
| The operator id is a configured claim, recorded beside the issuer | Ids are only unique per issuer, and switching IdPs is then a visible migration, not a silent remap |
| No role, group or allow-list check in this gateway; any operator may call any route until a decision service is wired in | A check here would become the policy engine by accretion. OpenID AuthZEN keeps the decision outside, with any engine behind it ([AuthZEN](https://openid.net/wg/authzen/)). Every call is recorded meanwhile |
| Plat5 Auth is the local staff IdP, as its own instance with its own database | The local stack shows the real shape: two issuers, never one |
