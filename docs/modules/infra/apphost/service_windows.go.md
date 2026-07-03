# Module: infra/apphost/service_windows.go

## Purpose

Windows-only (`//go:build windows`) Service Control Manager integration for `Run`, so `mymatasan.exe` can be registered as a native Windows service (see `packaging/windows/mymatasan.iss`) and controlled from `services.msc`/`sc.exe` without a wrapper such as WinSW/NSSM.

## Responsibilities

- `runWithPlatform(app)`: calls `svc.IsWindowsService()`; when false (interactive/dev run, or launched by another supervisor) it calls `runApp(app)` directly, same as any other platform. When true, it allocates `windowsServiceStop` and calls `svc.Run(app.Name(), &windowsService{app: app})`, handing control to the SCM.
- `platformShutdownChan()`: exposes `windowsServiceStop` to `runApp`'s shutdown `select` in `run.go`. It is `nil` (and therefore inert in a `select`) until a service run allocates the channel.
- `windowsService.Execute`: the `golang.org/x/sys/windows/svc` handler.
  - Reports `StartPending`, starts `runApp(app)` in a goroutine, then reports `Running` accepting `Stop`/`Shutdown`.
  - On `Stop`/`Shutdown`: reports `StopPending`, closes `windowsServiceStop` (triggers `runApp`'s graceful shutdown), waits for `runApp` to return, then reports `Stopped`.
  - On `Interrogate`: echoes the current status back to the SCM.
  - If `runApp` returns on its own (a fatal startup error, since a supervised restart `os.Exit()`s before returning), reports `Stopped` and exits with code `1` on a non-nil error, `0` otherwise.

## Notes

- Only compiled on Windows (`//go:build windows`); `service_other.go` provides the non-Windows counterpart.
- `windowsServiceStop` is package-level so `platformShutdownChan()` (called from `run.go`, same package) can read it without threading it through `App`/`Dependencies`.
- The installer (`packaging/windows/mymatasan.iss`) registers the service via `sc.exe create ... binPath= "<exe>" -app mymatasan` with `LocalSystem` and per-service `Environment` (home/data split + `KOPIV2_SUPERVISED=1`), so a service-managed run relies on the same supervised-restart contract as systemd/Docker.
