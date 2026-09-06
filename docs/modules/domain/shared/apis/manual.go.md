# Module: domain/shared/apis/manual.go

## Purpose

Shared HTTP handler set for an app's built-in user manual, built on top of
`domain/shared/manual.Library` (`manual/manual.go.md`). Serves the article index, one
article, the whole book in one payload (for client-side search and print), and figure
assets. Everything served is shipped, read-only reference content compiled into the
binary — no runtime state, no per-user data — so there is nothing here to leak between
roles.

## Behavior

- `ManualHandlers{lib, corpusOnce, corpus}` / `NewManualHandlers(lib)` — constructs the
  handler set over one app's manual library. `corpus` is a `retrieval.Corpus`
  (`manual/retrieval/retrieval.go.md`) built lazily by `index()` on the first `Search` call
  (`sync.Once`), over exactly one `retrieval.Source{App: "self", Library: lib}` — a reader
  who only ever clicks through the contents pays nothing for it, and unlike the fleet
  agent's multi-app corpus, an appliance searching its own manual has only one possible
  answer to "which product is this from".
- `List(w, r)` — `GET` handler. Resolves the requested `?lang=` (`language(r)`, defaulting
  to English inside the library — no validation needed at this layer) and returns
  `{language, languages, items}` where `items` is `lib.Articles(lang)` (metadata only).
- `Bundle(w, r)` — `GET` handler. Same shape as `List` but `items` is `lib.Bundle(lang)`
  (bodies included) — the client fetches this once and then searches/prints entirely
  offline, with no per-article round trip and no server-side search index to keep in step
  with the content.
- `Search(w, r)` — `GET` handler for `?q=&lang=&limit=`. Ranks the query with BM25 over the
  same retrieval engine the myseliasan fleet agent already grounds its answers in (nothing
  new is indexed), returning the matching **section** rather than the whole article —
  `{language, query, items: [{slug, anchor, title, heading, language, snippet}]}`. `limit`
  defaults to 8 and is clamped to 1–25. A query under two runes returns `items: []` rather
  than ranking the whole manual by noise — a reader still typing is not yet a question.
  Each result's own `language` may differ from the requested one: a question with little to
  say in ms/zh/ar falls back to English, same as the fleet agent's retrieval does, because
  operators type English product nouns (ONVIF, RTSP, Modbus) inside a non-English sentence
  constantly.
- `Get(w, r, slug)` — one article by slug (caller passes `slug`, since the mux variable
  name is the app's own route-registration choice, not this package's); `404` via
  `controllers.ErrNotFound` when the slug doesn't exist for the resolved language.
- `Asset(w, r, name)` — serves a manual figure verbatim. `Content-Type` resolved from the
  extension (`mime.TypeByExtension`) with a generic-binary fallback, always paired with
  `X-Content-Type-Options: nosniff` so a figure can never be coaxed into executing in the
  browser; cached `public, max-age=3600` since embedded content only changes when the
  binary does.
- `language(r)` — reads `?lang=` off the query string, lower-cased/trimmed.

## Notes

- **Route registration is deliberately left to each app**, not shared, the same way
  `SetupHandlers` (`apis/setup.go.md`) does it — the apps differ in where the manual sits
  in their middleware chain.
- `apps/mymatasan/apis/manual.go` (`apps/mymatasan/apis/manual.go.md`) and
  `apps/myseliasan/apis/manual.go` (`apps/myseliasan/apis/manual.go.md`) both mount this
  set on the **public**/bare router, before any auth middleware, because the sign-in
  screen and the first-run wizard — two of the places a reader most needs the manual —
  are places they either cannot authenticate yet or are stuck trying to.
