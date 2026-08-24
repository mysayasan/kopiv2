---
title: Case files
category: daily-use
categoryLabel: Daily use
summary: Collect the footage, sightings and notes for one incident in one place — and keep that footage safe from retention until you are done with it.
order: 170
---

# Case files

Everything this appliance records is organised by camera and by time. An incident is
neither. It is one person crossing four cameras over eleven minutes, the alert that fired
halfway through, and the two things you noticed afterwards.

A **case** is the container for that. It holds the clips, the sightings and your notes; it
can be handed to a colleague; it closes with a stated outcome; and it exports as a single
verifiable file you can give to somebody else.

It also does one thing no folder of downloaded clips can do: **it keeps its footage.**

## Opening a case {#opening}

**Cases → New case**, and give it a title. That is the whole ceremony — a case with a title
can be filled in as you go.

Assign it to yourself or a colleague from the case's **Assigned to** list. Assignment is
recorded in the audit trail, so "who was handling this" has an answer later.

## Adding evidence {#adding}

Evidence is added from wherever you found it, not typed into the case by hand:

- **Timeline → Add to case** bookmarks the moment you are watching, on every camera on
  screen. That is usually what an incident looks like: one instant, several angles.
- **Objects → Add to case** adds a recorded sighting.
- **Add note**, inside the case, records something with no footage behind it — a
  registration number, what a witness said, what you concluded.

Every piece of evidence carries a note. Use it. A case of six clips with no notes tells the
next person what was recorded, not what it means.

> [!NOTE]
> Adding evidence copies the *time and camera*, not the video. The video stays where it is
> — which is why the next section matters.

## Footage in a case is not deleted {#holding}

While a case is **open**, the footage its evidence points at is held: retention will not
delete it, the per-camera **Purge now** will not delete it, and neither will the automatic
clean-up that runs when the disk gets full.

This is the reason to open a case at all. With seven-day retention, a case opened today
would otherwise be a list of broken links next week.

The case says what it is holding, at the top: how many clips, how much disk, and — the
number worth reading — **how many of them are only still here because this case is open**.
Those are the clips already past their retention date.

> [!NOTE]
> A **secure wipe**, a **factory reset**, and **deleting the camera** all still destroy
> footage regardless of any case. Those are the "destroy everything" operations and a case
> is not a lock against them.

Removing a piece of evidence, or closing the case, releases that footage back to the normal
retention policy. Nothing is deleted at that moment — the footage simply goes back to the
lifetime it would have had if the case had never existed, and the next clean-up applies to
it like anything else.

## Exporting the case {#exporting}

**Build bundle**, with a reason, produces one `.zip` containing:

- `clips/` — every piece of footage in the case, joined without re-encoding.
- `manifest.json` — what each clip is, which recorded segments it came from, the SHA-256 of
  each, and any period inside a clip that has no recording.
- `chain-of-custody.csv` — every recorded action on this case, in order: who opened it, who
  added what, who annotated, who exported.
- `VERIFY.txt` — how to check all of the above with standard tools.

The reason is required, and the export is written into the audit trail twice: once when it
is requested and once when the file is actually downloaded.

If a piece of evidence has no footage left, the bundle is **still produced** and that clip
is listed as missing, with why — on the screen and inside the file. An export that quietly
left it out would look complete.

Exporting is available to operators as well as administrators. Deleting footage is not —
see [Users and roles](users-and-roles).

## Closing a case {#closing}

**Close case** asks for an outcome, and requires one. A case closed with no stated outcome
is indistinguishable from a case somebody tidied away.

The dialog restates what closing releases, including how many clips are already past their
retention date and will go on the next clean-up. **If you need those, export the case
first.**

A closed case can be reopened, which holds its footage again — for anything that is still
there.
