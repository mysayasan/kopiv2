---
title: Users, roles and access
category: admin
categoryLabel: Administration
summary: Nobody gets in by signing in — and menus and API permissions come from one place.
order: 510
---

# Users, roles and access

## A new sign-in gets nothing {#pending}

Someone who signs in successfully arrives with **no role** and sees an *access pending* screen
rather than the control plane.

That is deliberate and it is the important part of this page. Authenticating proves who somebody
is; it does not say what they may see. Until an administrator assigns a role, a new account can
open nothing — so an identity provider that provisions accounts liberally cannot quietly hand
somebody your fleet.

Clearing them is one action: pick a role in the **Users** list.

## Managing users {#users}

The list shows each account's **Kind** (a local account, or one federated through myidsan), its
role, and whether it is active.

- **Assign a role** — the normal clearance action.
- **Disable** — keeps the account and revokes its access. This is what you want for somebody who
  has left; deleting an account loses the trail of what it did.
- **Make superadmin** — grants full bypass. Use it sparingly and never as a shortcut for a
  permission you could grant properly.

## Retiring the stock superadmin {#handoff}

The account you first signed in with is a **stock** superadmin, and it should not stay in service.

Create a real superadmin account, confirm it works, then disable the stock one — the **bootstrap
handoff**. The control plane nags you about this only once a real superadmin is active, so the
prompt appears exactly when it is safe to act on.

## Roles {#roles}

Roles come in two kinds: **built-in** roles, which cannot be deleted, and **custom** ones you
create.

A new role starts as a **viewer**: read-only on the fleet and notifications, plus viewer access on
every node adopted so far. Starting from something coherent and narrowing beats starting from
nothing and discovering the gaps one complaint at a time.

You can rename a role, and **copy** one — the fastest way to make "the same as operators, plus
reports" without rebuilding it by hand.

## Access: features first, paths if you must {#access}

**Access** grants a role control-plane capabilities as plain switches — view the fleet and cameras,
manage the fleet, notifications, the AI agent.

**Advanced** grants specific API path prefixes and verbs for anything the switches do not cover.
Most roles never need it.

Two rules govern the whole matrix:

- **The longest matching prefix wins.**
- **No rule means denied.** A role with no rules is denied everything and sees no menus at all.

Deny-by-default is why a new capability added by an upgrade does not quietly become available to
everybody.

**Superadmin bypasses every check**, so its matrix is empty by design — there is nothing to
configure and nothing to get wrong.

## Menus and permissions are the same thing {#menus}

**Menu access** toggles which navigation sections a role can see, and each switch grants or revokes
**GET on that section's API path**.

This is worth understanding rather than skimming: there is no separate "UI permission". The nav is
rendered from the same matrix that guards the API, so a role can never see a page it cannot call,
and hiding a menu is not a security measure applied on top of a real one — it *is* the real one.

The practical consequence: if somebody cannot see a page, grant the capability rather than hunting
for a display setting, because there isn't one.

## Node access is a separate question {#node-access}

The matrix decides what somebody may do **on this control plane**. It does not decide what they may
do **on a node**.

Node access is granted per node, at one of the node's own levels — viewer (read-only), operator
(read plus limited write) or admin (read and write). The role that adopted a node always has full
access to it.

Keep the two straight: a role can be allowed to see that a node exists here while having no access
to the appliance itself, which is exactly right for somebody who monitors a fleet but must not
reconfigure cameras.

## When a colleague cannot see a page or a menu {#troubleshooting}

A missing menu entry is a missing permission — there is no display setting. In order of likelihood:

1. **They have no role yet** — the pending screen. Assign one.
2. **Their role has no rule for that path**, and no rule means denied.
3. **The menu switch for that section is off**, which is the same thing as the rule being absent.
4. **It is node access they lack, not control-plane access** — they can see the fleet but not drive
   that appliance.
5. **The account is disabled.**

Every one of these changes is recorded in the [audit log](audit-log).
