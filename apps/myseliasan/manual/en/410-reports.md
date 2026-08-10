---
title: Reports
category: reports
categoryLabel: Reports
summary: Four printable PDFs, rendered on the control plane itself — and the one limitation to know.
order: 410
---

# Reports

**Reports** produces printable PDFs of your fleet on demand: preview one on screen, then print or
download it.

They are rendered **on the control plane itself**. No external service, no headless browser, no
font downloaded at render time — which is what lets an air-gapped site produce a report at all.

## The four reports {#reports}

**Fleet Health** — every node's online/offline status, the certificate expiry roster, and an alert
summary over the period. This is the one to attach to a monthly operations review, and the
certificate roster is the part people are glad to have seen in advance.

**Site & Asset Inventory** — the asset register per building, with rendered floor plans, camera
placements and on-site appliances. This is worth exactly as much as your
[floor plans](buildings-and-floors) are accurate; it is a printout of what you drew.

**Incident Detail** — recent alerts over the period with per-event detail and snapshots carried
inline. The one you hand to somebody investigating a specific night.

**Security & Access** — users, roles, the endpoint permission matrix, the audit trail, and a
data-protection attestation. **Superadmin only**, because it is a complete description of who can
do what.

## Period and site {#scope}

Every report takes a **period**, and can be limited to one **site**.

Reporting per site is the normal case for a multi-location fleet: the person who runs one warehouse
wants that warehouse, and a fleet-wide PDF is a document nobody reads to the end.

## Preview, print, download {#output}

**Preview** renders it on screen so you can check the scope before committing it to paper.

**Print** opens the browser's print dialog; if the browser blocks the popup, the page says so and
**Download** gives you the same PDF as a file. Download is also the right choice when the report is
going somewhere by email or into a records folder.

## The one limitation to know {#latin-only}

Report text is drawn with a built-in Latin-script font, which covers **English, Malay and accented
European** text.

**CJK and Arabic characters in user-entered names render blank.** A building called 仓库 A or a node
named بوابة will produce an empty space in the PDF where its name should be.

This is a known, deliberate v1 limitation rather than a silent failure, and there is a practical
way around it: if your reports go to readers in those languages, give your buildings, areas and
nodes Latin-script names — or keep a Latin name alongside the local one — and the reports stay
legible. The rest of the product is fully translated; it is only the PDF that is constrained, and
only until a Unicode font is bundled.

## Permissions {#permissions}

Generating a report is a granted capability like any other, and **Security & Access is superadmin
only** regardless of what else a role has been given. If a report is refused, that is the matrix
doing its job — see the roles page rather than assuming a fault.
