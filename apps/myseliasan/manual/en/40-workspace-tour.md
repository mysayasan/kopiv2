---
title: A tour of the workspace
category: getting-started
categoryLabel: Getting started
summary: What every part of the screen is for, and why your menu differs from a colleague's.
order: 40
---

# A tour of the workspace

Every screen shares the same frame: a navigation rail down one side, a thin action strip across
the top.

## The side rail {#side-rail}

**Workspace**

- **Dashboard** — the summary screen for the whole fleet.
- **AI Insight** — the narrated digest, and ask-the-fleet chat. Appears only where granted.
- **Map** — your sites on a geographic map, and floor plans inside them.

**Fleet**

- **Live Views** — the video wall, spanning every node you can see.
- **Objects** — search what the fleet's cameras saw, across nodes.
- **Teach** — teach a camera something the stock model does not know.
- **Nodes** — the expandable tree. Its root lists your appliances; each node beneath opens that
  node's own pages, over the tunnel.
- **Fleet rules** — the only entry that is about the fleet *as a whole*: a rule spanning a camera
  node and a sensor hub.

**Administration**

- **Users**, **Roles & Access**, **Audit Log**.

**System**

- **Notifications** — the shared feed, with an unread count.
- **Reports** — generated PDFs.
- **Settings** — superadmin only.
- **Help** — this manual.

## Why your menu differs from a colleague's {#menu-differences}

This is the part worth understanding, because it works differently here than on a single
appliance.

**The rail is built from your permissions.** An entry appears because your role is granted the API
behind it — the Nodes branch needs read access to nodes, AI Insight needs access to the agent —
not because of a flag on your account. Menu and enforcement come from one source, so the rail
cannot offer something the server will refuse.

The practical consequence: **if you expected an entry and it is not there, it is your role.** Ask
an administrator rather than looking for the page.

And unlike a single recorder with three fixed roles, roles here are **yours to define**. A control
plane usually has several kinds of user — somebody who watches, somebody who administers a site,
somebody who only reads reports — so the model expects you to create them rather than choose from
a list.

## Node access is separate {#node-access}

A role grants access to *pages*. Access to a *particular node* is granted on top of that.

That separation is what lets one control plane serve several sites without everyone seeing
everything: a role can carry live views and notifications generally, while a person is granted
only the two nodes at the depot they actually work at.

A newly adopted node is therefore not automatically visible to everyone — see
[after adoption](adopting-nodes#after).

## The account block {#account}

Under the logo: your role and a sign-out button. It shows the *role*, not the identity — a control
plane is often on a screen other people can see, and there is no reason to advertise which account
is signed in.

## The top strip {#header}

- **Language** — English, Malay, Chinese, Arabic. Arabic mirrors the whole layout.
- **Theme** — light or dark.
- **?** — opens this manual at the page for whatever you are looking at.

Both preferences are instant and remembered on this browser, so a shared terminal can be left in
whatever language the person using it reads.

## Things that appear over the top {#overlays}

- **Toasts** — brief confirmations in a corner; they disappear on their own.
- **Full-screen progress** — only for operations that must not be interrupted, such as a restart
  after a settings change. Leave the page open; it reloads by itself.
