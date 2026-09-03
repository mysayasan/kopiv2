# Module: infra/apphost/browser.go

## Purpose

Opens a browser on the primary URL after `announce.go.md`'s ready banner prints — a
convenience for the person who just typed the start command, and NOT something to do on
every kind of start: a Windows service has no desktop to open one on, a container has no
browser at all, and a self-restart after a settings change would pop a new tab every time
the operator saves. Each of those is an explicit guard here rather than an accepted
annoyance.

## `browserSuppressedBecause() string`

Returns a human-readable reason not to open a browser, or `""` when opening is
appropriate. The reason is printed in the ready banner, so an operator who expected a
browser is told why they did not get one instead of wondering. Checked in order:

1. `KOPIV2_OPEN_BROWSER` explicit value wins over everything below it — `0`/`false`/
   `no`/`off`/`never` suppresses with that reason; `1`/`true`/`yes`/`on`/`force` forces
   opening unconditionally (someone running under X-forwarding or remote desktop knows
   better than the heuristics below).
2. `KOPIV2_NO_BROWSER` set (any value) suppresses.
3. Running as a platform service (`platformSupervised()` — a Windows service on Windows).
4. `KOPIV2_SUPERVISED` set — running under a process supervisor (systemd/Docker).
5. `relaunchedByRestart` — this process was started by a previous instance's self-restart
   (a settings-change restart, not a fresh start).
6. `inContainer()` — the common container markers (`KUBERNETES_SERVICE_HOST`,
   `/.dockerenv`, `/run/.containerenv`); best-effort, a false negative only costs a
   harmless failed launch attempt that gets logged.
7. `!hasDesktopSession()` — Windows/macOS always have one (the service case is caught
   earlier); Unix needs `DISPLAY` or `WAYLAND_DISPLAY`.

## `relaunchedByRestart`

Package-level flag, set exactly once in `run.go`'s `runApp` — right where the
`KOPIV2_RESTART_DELAY_MS` marker env var is read and unset, before it can propagate to any
future restart. Records that this process is a self-relaunch rather than an operator's
fresh start.

## `launchBrowser(url string) (opened bool, reason string)`

The seam `infra/apphost/firstboot` also uses (`Options.Browser`, `firstboot/
firstboot.go.md`) — the exact same suppression rules apply to the pre-boot wizard's
browser launch as to the app's own ready banner. Checks `browserSuppressedBecause` first;
otherwise calls the swappable `browserOpener` (`startDetached` by default; a test can
substitute it to assert what would be run without actually opening anything).

## `startDetached(url string) error`

Per-OS: `rundll32 url.dll,FileProtocolHandler <url>` on Windows (not `cmd /c start`, which
needs an empty-title argument to survive a quoted URL and flashes a console window on a
GUI-less start — the same nuisance the self-restart path already had to fix; not
`DETACHED_PROCESS`, which allocates a new console window per relaunch); `open` on macOS;
`xdg-open` elsewhere.

## Notes

- `inContainer`/`hasDesktopSession` are best-effort environment checks; every false
  negative only costs a harmless suppressed (or attempted-and-logged-failed) browser
  launch, never a boot failure.
