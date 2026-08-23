# W3-2 bench, part 1: does the appearance stage actually DISCRIMINATE?
#
# Everything else in this feature — storage, ranking, federation — is arithmetic over
# vectors, and arithmetic over vectors that all look alike ranks nothing. So before any of
# that is worth benching, the claim underneath it has to hold: two crops that look alike
# must embed closer together than two crops that do not, using the REAL model on REAL
# pixels through the REAL worker function.
#
# It runs `_appearance_embed` directly rather than driving YOLO, and that is deliberate
# rather than a shortcut. The bench harness has no people in front of its cameras — its
# sources are synthetic test patterns — so the detector would find no person or vehicle to
# describe and the stage would correctly do nothing. Feeding the stage hand-made detections
# over hand-made crops tests exactly the thing that is in question (the embedding and its
# discrimination) and nothing that is not.
#
# WHAT THIS DOES NOT CLAIM, stated because the gap matters: it does not prove that a real
# camera watching a real person produces a stored descriptor. That path needs footage of
# people, which this harness does not have.
import json, os, subprocess, sys, tempfile

REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CHECKS = []


def check(name, ok, detail=""):
    CHECKS.append((name, bool(ok), detail))
    print(("PASS  " if ok else "FAIL  ") + name + ("   " + detail if detail else ""))


def report():
    ok = sum(1 for _, good, _ in CHECKS if good)
    print("\n%d/%d checks passed" % (ok, len(CHECKS)))
    for name, good, detail in CHECKS:
        if not good:
            print("  FAILED: %s   %s" % (name, detail))
    return 0 if ok == len(CHECKS) else 1


# The scene: four panels across one frame. Two are the same distinctive subject rendered
# slightly differently (a red figure, shifted and re-shaded — the same person seen twice,
# not the same pixels twice); one is a clearly different subject (a blue figure); one is
# background. If the embedding cannot separate those, nothing downstream can.
#
# Drawn with PIL inside the bench-ffmpeg image, which already carries torch and torchvision
# for the anomaly feature — the same dependency appearance search rides on.
SCENE = r'''
import json, sys
from PIL import Image, ImageDraw

W, H = 1280, 480
img = Image.new("RGB", (W, H), (28, 30, 34))
d = ImageDraw.Draw(img)

def figure(x0, base, torso, legs, shift=0, shade=0):
    # A crude standing figure: head, torso, legs. `shade` nudges every channel so the two
    # "same subject" panels are not pixel-identical — a match on identical bytes would
    # prove only that the model is deterministic.
    c = lambda t: tuple(max(0, min(255, v + shade)) for v in t)
    d.ellipse([x0 + 70 + shift, base + 30, x0 + 130 + shift, base + 90], fill=c((222, 190, 170)))
    d.rectangle([x0 + 55 + shift, base + 95, x0 + 145 + shift, base + 250], fill=c(torso))
    d.rectangle([x0 + 65 + shift, base + 250, x0 + 95 + shift, base + 380], fill=c(legs))
    d.rectangle([x0 + 105 + shift, base + 250, x0 + 135 + shift, base + 380], fill=c(legs))

RED, BLUE, DARK = (196, 52, 44), (44, 76, 190), (38, 40, 52)
figure(0, 40, RED, DARK, shift=0, shade=0)      # panel 0: subject A
figure(320, 40, RED, DARK, shift=14, shade=12)  # panel 1: subject A again, moved + reshaded
figure(640, 40, BLUE, DARK, shift=6, shade=-6)  # panel 2: subject B, clearly different
                                                 # panel 3: background only
img.save(sys.argv[1], "JPEG", quality=92)
print("ok")
'''

# The four crops as normalised boxes, matching the panels above.
BOXES = [
    {"x": 0.030, "y": 0.083, "w": 0.125, "h": 0.812},
    {"x": 0.280, "y": 0.083, "w": 0.125, "h": 0.812},
    {"x": 0.530, "y": 0.083, "w": 0.125, "h": 0.812},
    {"x": 0.780, "y": 0.083, "w": 0.125, "h": 0.812},
]

DRIVER = r'''
import json, sys, importlib.util, math

spec = importlib.util.spec_from_file_location("yw", sys.argv[1])
yw = importlib.util.module_from_spec(spec)
# The worker reads model paths from the environment at import; none are set here, which is
# fine — nothing but the shared crop backbone is touched.
spec.loader.exec_module(yw)

image_path = sys.argv[2]
boxes = json.loads(sys.argv[3])
dets = [{"label": "person", "confidence": 0.9, "box": b} for b in boxes]
n = yw._appearance_embed(image_path, dets)

def cos(a, b):
    if not a or not b or len(a) != len(b):
        return 0.0
    dot = sum(x * y for x, y in zip(a, b))
    na = math.sqrt(sum(x * x for x in a))
    nb = math.sqrt(sum(y * y for y in b))
    return dot / (na * nb) if na and nb else 0.0

vecs = [d.get("appearance") for d in dets]

# Score the two halves separately as well as together. The whole reason the colour block
# exists is that the shape half could not separate a red figure from a blue one, so the
# bench must be able to show what each half contributes rather than only the total.
SHAPE = 512
shape = [v[:SHAPE] if v else None for v in vecs]
colour = [v[SHAPE:] if v else None for v in vecs]

out = {
    "embedded": n,
    "model": next((d.get("appearanceModel") for d in dets if d.get("appearanceModel")), ""),
    "dims": [len(v) if v else 0 for v in vecs],
    "sameSubject": cos(vecs[0], vecs[1]),
    "differentSubject": cos(vecs[0], vecs[2]),
    "vsBackground": cos(vecs[0], vecs[3]),
    "shapeSame": cos(shape[0], shape[1]),
    "shapeDifferent": cos(shape[0], shape[2]),
    "colourSame": cos(colour[0], colour[1]),
    "colourDifferent": cos(colour[0], colour[2]),
    "unitLength": [round(math.sqrt(sum(x * x for x in v)), 4) if v else 0 for v in vecs],
}
print("RESULT " + json.dumps(out))
'''


def main():
    workdir = tempfile.mkdtemp(prefix="w32bench")
    scene_py = os.path.join(workdir, "scene.py")
    driver_py = os.path.join(workdir, "driver.py")
    open(scene_py, "w", encoding="utf-8").write(SCENE)
    open(driver_py, "w", encoding="utf-8").write(DRIVER)

    scene_jpg = os.path.join(workdir, "scene.jpg")
    worker = os.path.join(REPO, "apps", "mymatasan", "ai", "yolo_worker.py")

    # Run on the HOST interpreter, not in a container.
    #
    # The appearance stage rides torch + torchvision, which the anomaly feature already
    # requires and which this host therefore has; none of the bench container images carry
    # them (they exist to provide ffmpeg). Installing a multi-gigabyte ML stack into a
    # throwaway image to test a function that runs on the host anyway would be benching a
    # different deployment from the one that ships.
    missing = []
    for mod in ("torch", "torchvision", "PIL"):
        r = subprocess.run([sys.executable, "-c", "import " + mod], capture_output=True, text=True)
        if r.returncode != 0:
            missing.append(mod)
    check("the host has the stack the appearance stage runs on", not missing,
          "missing: " + ", ".join(missing) if missing else "torch + torchvision + PIL")
    if missing:
        return report()

    r = subprocess.run([sys.executable, scene_py, scene_jpg], capture_output=True, text=True)
    check("the test scene renders", "ok" in r.stdout, (r.stdout + r.stderr)[:300])
    if "ok" not in r.stdout:
        return report()

    # Run the REAL appearance stage over it.
    r = subprocess.run(
        [sys.executable, driver_py, worker, scene_jpg, json.dumps(BOXES)],
        capture_output=True, text=True)
    line = next((l for l in r.stdout.splitlines() if l.startswith("RESULT ")), "")
    check("the appearance stage ran inside the worker", bool(line),
          (r.stdout[-400:] + " | " + r.stderr[-400:]))
    if not line:
        return report()
    res = json.loads(line[len("RESULT "):])
    print("measured: " + json.dumps(res, indent=2))

    check("every eligible crop got a descriptor", res["embedded"] == 4,
          "embedded=%s" % res["embedded"])
    check("the descriptors are the model's full width (512 shape + 48 colour)",
          all(d == 560 for d in res["dims"]), "dims=%s" % res["dims"])
    check("the descriptors are stamped with the model that made them",
          res["model"] == "resnet18-hsv-560", "model=%r" % res["model"])
    # L2-normalised at the source. The ranking normalises again defensively, but if the
    # producer stopped doing it the stored vectors would drift in magnitude and every
    # similarity threshold calibrated against them would quietly mean something else.
    check("the descriptors come out unit length",
          all(abs(u - 1.0) < 0.01 for u in res["unitLength"]),
          "norms=%s" % res["unitLength"])

    # THE CLAIM THE WHOLE FEATURE RESTS ON.
    check("the same subject scores higher than a different one",
          res["sameSubject"] > res["differentSubject"],
          "same=%.4f different=%.4f" % (res["sameSubject"], res["differentSubject"]))
    check("the same subject scores higher than empty background",
          res["sameSubject"] > res["vsBackground"],
          "same=%.4f background=%.4f" % (res["sameSubject"], res["vsBackground"]))
    # THE MEASUREMENT THAT DECIDED HOW THIS FEATURE SCORES, pinned so it cannot drift
    # unnoticed.
    #
    # The ordering above is correct, but the ABSOLUTE numbers are almost identical: a red
    # figure and a blue one score ~0.95, barely below the ~0.98 of the same figure twice.
    # ImageNet features are dominated by structure — a person-shaped thing on a dark
    # background — and discard most of what an operator means by "the man in the red
    # jacket". That is why the product scores a hit by how far it stands out from the other
    # candidates rather than by an absolute threshold, and why the screen never prints a
    # match percentage.
    #
    # This asserts the PREMISE rather than a margin. If a future model produces genuinely
    # spread-out similarities, this check fails — and that failure is the signal to revisit
    # the relative scoring, which would then be solving a problem that no longer exists.
    margin = res["sameSubject"] - res["differentSubject"]
    shape_margin = res["shapeSame"] - res["shapeDifferent"]
    colour_margin = res["colourSame"] - res["colourDifferent"]

    # WHAT THE COLOUR BLOCK IS WORTH, measured rather than asserted. The shape half alone is
    # the thing that failed: an ImageNet backbone is trained for class invariance, so it
    # answers "person" whichever jacket the person is wearing. This pins the improvement, so
    # a change that quietly neuters the colour contribution shows up as a failure here
    # instead of as slightly worse rankings nobody traces back.
    check("the colour half separates the two subjects far better than the shape half",
          colour_margin > shape_margin * 2,
          "colour margin %.4f vs shape margin %.4f" % (colour_margin, shape_margin))
    check("adding colour measurably widens the overall separation",
          margin > shape_margin * 1.8,
          "combined %.4f vs shape-only %.4f (%.1fx)"
          % (margin, shape_margin, margin / shape_margin if shape_margin else 0))

    # THE PREMISE BEHIND RELATIVE SCORING, still true and still pinned. Even with colour, two
    # unrelated people score 0.85 against each other while a true match scores 0.92 — the
    # usable range is a sliver near the top, so an absolute threshold remains the wrong tool
    # and the screen still must not print a match percentage. If a future change genuinely
    # spreads these out, THIS check fails, and that failure is the signal to revisit the
    # relative scoring rather than a regression to fix.
    check("unrelated subjects still score high in absolute terms (the reason scoring is relative)",
          res["differentSubject"] > 0.75 and margin < 0.25,
          "different subjects = %.4f, margin over a true match = %.4f — an absolute "
          "threshold still cannot separate these" % (res["differentSubject"], margin))
    check("background is clearly further away than either subject",
          res["differentSubject"] - res["vsBackground"] > 0.15,
          "different=%.4f background=%.4f" % (res["differentSubject"], res["vsBackground"]))
    return report()


if __name__ == "__main__":
    sys.exit(main())
