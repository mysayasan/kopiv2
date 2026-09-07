---
title: Connecting a directory
category: people
categoryLabel: People and access
summary: Sign people in with the LDAP or Active Directory account they already have.
order: 220
---

# Connecting a directory

MyIDSan can check passwords against an LDAP or Active Directory server you already run, so people
sign in with the domain account they have rather than a second one they have to remember. The
**Directory** screen configures it, and there is only ever one directory.

## What MyIDSan does and does not take from the directory {#scope}

It takes **authentication** — is this the right password — and **group membership**. It does not
copy your directory into its own database, and it does not write to your directory. The account
MyIDSan keeps is a local shell holding the role and the audit history; the credential stays where
it always was.

Passwords are never stored for directory accounts. Neither is the *user's* password ever seen
beyond the bind that checks it.

## Filling in the form {#form}

- **Server host** and **Port**, with **StartTLS** on for a plaintext port that is upgraded, off
  for implicit TLS (`ldaps`).
- **Pinned CA certificate** — paste the PEM if the directory's certificate is signed by your own
  authority. This is usually what a connection failure turns out to be.
- **Service account bind DN** and **password** — the read-only account MyIDSan binds as to find
  users. The password is write-only: leave the field blank to keep the stored one, so saving an
  unrelated edit never means retyping the secret.
- **Base DN** — where to search from.
- **User filter** — with `%s` standing in for the username being looked up.
- **Group attribute** — the attribute holding the user's groups. On Active Directory this is
  usually `memberOf`.
- **Subject attribute** — the attribute that identifies the account **for the rest of its life**.
  See below; this is the one field worth getting right first time.
- **Login option label** — the wording on the button on the sign-in screen. "Domain account" beats
  "LDAP" for the people who have to click it.

**Test connection** runs against the settings currently in the form, not the saved ones, so you
can prove an edit before committing it. Give it a sample username and it will report the groups it
found — which is what you need before writing any mapping.

> [!IMPORTANT]
> Pick the subject attribute to be something **immutable** — an object GUID rather than an email
> address or a username. It is what a returning person is matched on, and matching on anything a
> person can be given a new one of means a rename hands them somebody else's account, or strands
> them with a new one and no role.

## Mapping groups onto roles {#mapping}

A directory group on its own grants nothing here. Add a mapping — group DN or name, to a role,
with a priority — and a person in that group gets that role.

- **Highest priority wins** when somebody is in several mapped groups.
- **Matching no mapping is not a refusal.** The person signs in successfully and lands on the
  waiting-for-clearance screen, exactly like any other new account. See
  [Users, roles and groups](users-roles-groups#pending).

**Directory is authoritative** decides what happens on the *second* sign-in. With it on, the
mapping is re-applied at every login: a role set by hand here is overwritten, and somebody removed
from a group in the directory loses the role it granted the next time they sign in. With it off,
the mapping seeds the role once and local edits stick.

Turn it on when the directory is where access is really decided — that is the point of connecting
one. Leave it off only if you intend to manage roles here, and then be honest that removing
somebody from a domain group will not remove their access.

## Second factors for directory accounts {#mfa}

A required-MFA policy does **not** extend to directory accounts unless you switch on
*apply to directory* as well. That default is deliberate: those users' factor policy usually
belongs to the domain, and enforcing one here duplicates something the domain already asks for.
See [Second factors and account recovery](second-factor#policy).

## When it does not work {#troubleshooting}

- **The connection fails** — most often the certificate. Either pin the CA PEM, or check StartTLS
  matches the port you gave. Test connection reports the underlying error rather than a generic
  failure.
- **The bind succeeds but no groups come back** — the group attribute is wrong, or the directory
  does not populate it for that account. Some servers do not fill `memberOf` unless the group
  itself is of a type that maintains it.
- **Everybody lands on waiting for clearance** — the mappings are matching nothing. Run the test
  with a sample username and copy a group string it actually returned.

Every change to this screen is written to [the audit log](audit-log), including the host, base DN
and the authoritative flag — never the bind password.

## Where to go next {#next}

- [Users, roles and groups](users-roles-groups) — what a role grants once the mapping has applied.
- [The audit log](audit-log) — directory changes and the sign-ins that followed.
