---
title: Users, roles and groups
category: people
categoryLabel: People and access
summary: Clearing a pending account, and how a role turns into a menu.
order: 210
---

# Users, roles and groups

## A new account can do nothing {#pending}

An account that has just been created — by an administrator, by self-registration, or by a first
sign-in through a connected directory — holds **no role at all**. Its owner sees a *waiting for
clearance* screen instead of the workspace and can do nothing until somebody assigns one.

That is deliberate. The alternative, inheriting some default level of access, means a
self-registered stranger is a user of your identity server the moment they type an email address.

To clear one: **Users**, find the account, set its role, save. The person's screen has a
**Check again** button; they do not need to sign out and back in.

> [!NOTE]
> If the account should not exist at all, set it inactive rather than assigning a role. Deleting it
> also works, but leaves the audit trail pointing at an account nobody can look up.

## Roles decide the menu, not just the API {#matrix}

A role is a list of rules, one per API path prefix, each granting some of GET, POST, PUT and
DELETE. **Longest matching prefix wins, and no rule means denied.**

The same rules build the navigation rail. Granting a role `GET` on `/api/user-credential` is what
makes **Users** appear for it; revoking it is what makes it disappear. There is no separate menu
configuration to keep in step — which is exactly why there is no way for the menu to promise
something the server then refuses.

Two consequences:

- **Two people on the same server see different rails.** A short menu is the system working. If a
  colleague can see a screen you cannot, compare roles, not browsers.
- **A broad grant is broader than it looks.** `GET` on `/api` matches every path in the product.
  Grant the specific prefixes instead.

**Superadmin** bypasses the matrix entirely. A handful of screens — Users, Groups, Roles, RBAC,
Audit log, Backup & restore, Settings — are superadmin-only regardless of what the matrix says,
because they can be used to grant privilege, read every username, or export the whole identity
store. Those are never delegated.

## Groups {#groups}

Groups organise ownership and hierarchy: who an account belongs to, and under which parent. They
are not a second permission system — access comes from the role. A group's most common job is
being the target a directory group maps onto, so that people arriving from your domain land
somewhere sensible; see [Connecting a directory](directory#mapping).

## Endpoints, and access tiers {#endpoints}

The **Endpoints** screen is the catalog the matrix is written against: every API path the product
serves, with a tier.

- **AuthOnly** — a session is required, then the matrix decides. Almost everything.
- **Public** — no session. Reserved for things that must work before anyone can sign in: the
  sign-in endpoints themselves, health checks, and this manual.
- **DevOnly** — not served outside a development build.

You rarely need to touch this screen. It exists so a new endpoint cannot be silently reachable
without appearing somewhere an administrator can see it.

## The bootstrap account is meant to be retired {#handover}

While the stock superadmin is still active, a banner says so at the top of the workspace. The
intended end state is that real people sign in as themselves and the bootstrap account is set
inactive.

Until then every action in the audit log is attributed to a shared account, which makes the trail
considerably less useful than it should be — "superadmin changed a role at 02:00" names nobody.

## Where to go next {#next}

- [Connecting a directory](directory) — accounts from LDAP or Active Directory.
- [The audit log](audit-log) — what a role change looks like afterwards.
- [Sessions and step-up](sessions-and-step-up) — ending somebody's session now.
