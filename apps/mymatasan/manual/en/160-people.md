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

## Enrolling {#enrolling}

Add a person by name, then add photos of them.

What makes it work:

- **Ten to thirty photos.** Fewer works, but less reliably.
- **Varied angles and lighting.** Photos that all look the same teach the system one appearance,
  and it will fail on any other. Include the lighting the cameras actually have.
- **Exactly one large face per photo.** A group photo is rejected, and a face that is a few pixels
  across carries no usable detail.

Photos taken from your own cameras, in the place recognition will happen, outperform good studio
photographs. Match the conditions rather than the quality.

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
