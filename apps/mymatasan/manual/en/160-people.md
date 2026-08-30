---
title: People and face recognition
category: daily-use
categoryLabel: Daily use
summary: Enrol people so cameras recognise them — and the consent obligations that come with it.
order: 160
---

# People and face recognition

Face recognition lets your cameras tell you *who* rather than only *someone*. Enrolling a person
is instant — there is no training step.

## Before you enrol anyone {#consent}

The appliance makes you accept this before the feature turns on, and it is not boilerplate.

Enrolling somebody stores a **faceprint**: a mathematical representation of their face. That is
**biometric data**. Under GDPR, BIPA and comparable laws in many jurisdictions, you generally need
that person's informed consent *before* you enrol them, and you are responsible for how it is
used afterwards.

Practical rules that keep you on the right side of it:

- **Only enrol people who have agreed.** Not "people who work here". Not "people we have photos
  of".
- **Tell them what it is for.** Consent to being recognised at a staff door is not consent to
  being tracked across a site.
- **Delete people who leave.** Deleting a person erases their faceprints.

Faceprints are encrypted at rest, and deleting a person removes them. That protects the data; it
does not create the permission to hold it.

If you are not certain you have consent, do not enrol. Every other detection feature in this
product works on *what* rather than *who*, and none of them carries this obligation.

## The one-time setup {#setup}

Face recognition uses two model files that are not bundled with the appliance — they are licensed
separately and the recognition model is about 37 MB. Until they are on the machine, enrolling a
photo is refused.

You do not need a command prompt for this. The People screen shows a **Face recognition needs a
one-time setup** panel with a **Download and set up** button whenever something is missing; the
same control is in **Settings › AI**. It fetches only what is absent, installs the `opencv-python`
package if the AI runtime lacks it, checks that the models actually load, and shows its log while
it works. Nothing needs restarting afterwards — enrol a photo straight away.

It needs outbound internet access to `github.com` for the download. On an appliance with no route
out, run `ai/setup.ps1 -Faces` (or `setup.sh`) on a machine that has one and copy the two `.onnx`
files into the folder the panel names.

If the panel says the **AI runtime** is missing, install that first (Settings › AI): the face
models are loaded by it, so there is nothing for them to run in until it is there.

## Enrolling {#enrolling}

Add a person by name, then add photos of them. Naming somebody does nothing on its own: a person
with no photos is not in the gallery the recogniser reads, so they are never matched. The roster
says so on their card, and the screen takes you straight to their photos when you add them.

There are two ways to add a photo, side by side in the same panel:

- **From this computer** — choose or drag in image files. Several at once is fine.
- **Take a photo** — use the camera on the computer you are sitting at. Look straight into it and
  fill the frame with the face.

Either way, only the faceprint and a small crop of the face are stored; the photo you supplied is
not kept. Each enrolled photo appears in the panel with its quality, and can be removed
individually — worth doing if a bad photo has crept in, because one bad faceprint degrades every
match afterwards.

The browser only offers a camera on an **HTTPS** address or on **localhost**. Opened over a plain
`http://` LAN address, the "Take a photo" option will say so rather than appearing broken; upload a
photo instead, or reach the recorder over HTTPS.

What makes it work:

- **Ten to thirty photos.** Fewer works, but less reliably.
- **Varied angles and lighting.** Photos that all look the same teach the system one appearance,
  and it will fail on any other. Include the lighting the cameras actually have.
- **Exactly one large face per photo.** A group photo is rejected, and a face that is a few pixels
  across carries no usable detail.
- **The whole head in frame.** Passport photos, phone portraits and full-size camera files all work,
  at any size — but a crop so tight that the chin or the crown is cut off gives the detector nothing
  to work with, and is the one framing that is still refused.

Photos taken from your own cameras, in the place recognition will happen, outperform good studio
photographs. Match the conditions rather than the quality.

## What happens when somebody is recognised {#what-happens}

A match is not a line in a log somewhere. Every one of these happens, in this order:

1. **An alert** is written, labelled with the person and the match confidence — *Aminah Yusof
   (94%)* — or *Unknown face* for somebody who is not enrolled. It carries a snapshot with the face
   boxed.
2. **An event clip** is recorded around the moment, if that camera is recording.
3. **A notification** goes to the bell and to whichever destinations you have set up (webhook,
   Telegram, MQTT), with the snapshot attached. `{{person}}` is available in notification templates.
4. Optionally, **a camera moves** to a saved position and **a relay fires** (siren, strobe, gate) —
   both configured on the rule itself, in the camera's **AI Detection** tab.

You see the results in **Notifications**, in the camera's alert log, and on the Timeline — not on
the People page, which is why the roster also shows each person's most recent sighting.

## Choosing what to alert on {#alert-modes}

Each camera's face rule asks one of three questions, chosen on the People page beside the camera's
switch:

- **Anyone enrolled** — tell me when somebody we know is here. A staff door.
- **Only chosen people** — tell me when one of *these* people is here. A watchlist; pick the names
  underneath. A rule with nobody on the list is refused, because it would alert on nobody.
- **Unknown faces (strangers)** — tell me when somebody we do *not* know is here. A perimeter. It
  names nobody; it reports that an unrecognised face appeared, which is the honest thing to say.

Different cameras usually want different answers. The same choice, plus the confidence floor and
the routing/relay/PTZ actions, is available in full in the camera's **AI Detection** tab.

## Choosing where it runs {#per-camera}

Recognition is enabled **per camera**, not globally. It runs only where you switch it on.

Keep that list short. Every camera doing recognition costs processing, and — more importantly —
recognising faces at a canteen camera when you only needed the staff entrance is exactly the
overreach the consent conversation was about. Enable it where there is a reason and nowhere else.

## When recognition is unreliable {#accuracy}

Almost always one of these, in this order:

1. **Not enough photos, or too-similar photos.** Add more, varied.
2. **The camera cannot see faces.** Mounted high and angled down gives you the tops of heads.
   Recognition needs faces roughly front-on and reasonably large in frame.
3. **Lighting.** Strong backlight — a doorway against daylight — silhouettes everyone. Fix the
   camera position or the lighting; no amount of enrolment compensates.

Treat a recognition as a strong hint, not proof. It is evidence to review footage with, not a
conclusion to act on by itself.

## Deleting {#deleting}

Deleting a person removes them and all their faceprints, and cannot be undone. Do it when somebody
leaves, or withdraws consent — and make sure somebody at your site owns that as a routine, because
nothing prompts you.
