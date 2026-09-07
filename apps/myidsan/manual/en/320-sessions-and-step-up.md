---
title: Sessions and step-up
category: security
categoryLabel: Security
summary: Where a session actually lives, how to end one now, and what re-authentication protects.
order: 320
---

# Sessions and step-up

## Where a session lives {#where}

A sign-in here lasts three days, so somebody moving between apps is not asked for a password
again. That makes the session itself worth protecting, and worth being able to end at once.

The session is **not** the row you see in a list. It lives in the cache; the database row exists
so the list can be drawn at all.

```arch
title : The cache is the session — the table is only an index of them
ext browser : The person's browser, holding nothing but a session cookie
box auth : The check that runs on every single request
store cache : The cache. This entry IS the session — removing it is what ends one.
store table : The database. One row per session, so a list can be drawn. A row alone never proves a session is alive.
box screen : The session list, in Profile and on the Users screen
browser -> auth : Every request, carrying the cookie
auth -> cache : Is this session still here?
auth --> table : Last seen, refreshed as it goes
screen -> table : Which sessions does this account have?
screen -> cache : ...and is each of them actually still alive?
```

This is why the list is trustworthy in the one direction that matters. Rows outlive their cache
entries — a session that simply expired leaves its row behind — so every listing is reconciled
against the cache and anything missing is reported as **ended**. Revoking deletes the cache entry
first and marks the row second: if the second step fails, the session is still dead, which is the
safe direction to fail in.

## Ending a session {#revoke}

- **Your own**, under **Profile**: each session shows the address it came from, the browser, when
  it started and when it was last seen. Yours is labelled, so you can end the others without
  ending the one you are using. **Sign out everywhere else** does that in one go.
- **Somebody else's**, from the **Users** screen. This is what to reach for when a laptop goes
  missing or somebody leaves, and it takes effect on their next request — not at the end of the
  three days.

Ending a session does not disable the account. If the account should not come back, set it
inactive as well; otherwise the person simply signs in again.

Both are written to [the audit log](audit-log), separately for one session and for all of them.

## Step-up re-authentication {#stepup}

A superadmin session can assign roles, clear anybody's second factor, issue a temporary password
for any account, and export or restore the entire identity store. For three days, on nothing more
than a cookie.

Step-up closes that. Before a small set of actions, the server asks you to prove you still hold
the credential — your password, plus a code if you have a factor enrolled. It is **not** a second
session: it is a short-lived mark on the session you already have, so somebody holding only a
stolen cookie cannot produce it.

It lasts **five minutes**, which is long enough to finish a batch of admin work without retyping a
password per click, and short enough that a walked-away laptop is not still elevated.

Today it is asked for before:

- exporting or restoring a backup,
- resolving a password-reset request (which issues a temporary password),
- clearing another account's second factor or its security keys.

The mark is derived from the session id and lives in the cache alongside it, so revoking a session
takes its elevation with it, and a server restart does not leave anyone elevated.

Both outcomes are audited — the successful re-authentication and the failed one. A run of failures
against step-up is what somebody probing a stolen session looks like.

## Where to go next {#next}

- [The audit log](audit-log) — sessions ended, and step-up attempted.
- [Second factors and account recovery](second-factor) — what step-up asks you for.
- [Backup and restore](backup-restore) — the action step-up guards most heavily.
