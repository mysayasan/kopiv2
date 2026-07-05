# r450k — product site

Standalone static marketing site for the **r450k** platform, built with React + Vite and
deployed to **Cloudflare Workers (Static Assets)**. It is decoupled from the Go backend; it
ships a static `dist/` bundle served by a tiny Worker that also handles the contact endpoint.

Content lives in per-locale files under [`src/content/`](src/content/) (`en.js`, `ms.js`, `zh.js`, `ar.js`). Edit copy in `src/content/en.js` for English; the non-English files derive their structure from the English module. The site supports four languages — **English**, **Melayu**, **中文**, and **العربية** (Arabic, RTL) — with prerendered, per-locale indexable URLs (`/`, `/ms/`, `/zh/`, `/ar/`).

## Local development

```bash
cd r450k
npm install
npm run dev           # http://localhost:5173 (client only, no prerender)
npm run build         # client build + SSR build + prerender → dist/
npm run build:client  # client bundle only (skips SSR/prerender)
npm run preview       # serve the built dist/ locally
```

Requires Node 22+ (see [`.nvmrc`](.nvmrc)) — Wrangler 4 needs Node ≥ 22.

## Project layout

```
r450k/
  index.html            # entry HTML + meta tags (template; locale variants overwritten at build time)
  vite.config.js
  wrangler.jsonc        # Cloudflare Worker + Static Assets config
  worker/
    index.js            # Worker entry: POST /api/contact + serves dist/ assets
  scripts/
    prerender.mjs       # build-time SSR prerender — emits dist/index.html, dist/ms/, dist/zh/, dist/ar/
  public/
    favicon.svg
    _headers            # security + cache headers (served by Workers assets)
    robots.txt, sitemap.xml, og-image.svg
  src/
    main.jsx            # client entry (hydrateRoot)
    entry-server.jsx    # SSR entry used by prerender.mjs
    App.jsx
    i18n/
      index.jsx         # LangProvider, useLang, useContent, LANGS, localePath
    content/
      en.js             # English copy — edit here; authoritative source of structure
      ms.js             # Malay
      zh.js             # Chinese Simplified
      ar.js             # Arabic (RTL)
    styles.css          # dark theme, mirrors the apps' --theme-dark palette; RTL overrides
    components/         # Logo, Icon, ContactWidget, LangDropdown, animation helpers
    sections/           # Nav, Hero, Features, HowItWorks, Tiers, Showcase, UseCases, Apps, FinalCta, Footer
```

## Multi-language i18n + SEO prerender

The site supports four languages with prerendered, per-locale static pages that are individually indexable:

| Locale | URL | Direction |
| --- | --- | --- |
| English | `https://r450k.com/` | LTR |
| Melayu | `https://r450k.com/ms/` | LTR |
| 中文 | `https://r450k.com/zh/` | LTR |
| العربية | `https://r450k.com/ar/` | RTL |

**How it works**: `npm run build` runs three steps in sequence: (1) Vite client build → `dist/`; (2) Vite SSR build of `src/entry-server.jsx` → `dist-ssr/`; (3) `node scripts/prerender.mjs` which imports the SSR bundle, calls `render(lang)` once per locale, and writes a fully populated HTML file to `dist/` (e.g. `dist/ar/index.html`). Each output has the correct `<html lang/dir>`, translated `<title>`/meta, `<link rel="canonical">`, `<meta property="og:url">`, and `<link rel="alternate" hreflang>` tags for all four locales plus `x-default`. `public/sitemap.xml` lists all four locale URLs with hreflang alternates. `dist-ssr/` is gitignored.

**Language switcher**: `src/components/LangDropdown.jsx` renders `English | Melayu | 中文 | العربية` as real `<a href>` links pointing at the per-locale paths (crawlable and functional without JS), with a JS intercept for instant no-reload switching via `window.history.pushState`. Selecting Arabic flips `<html dir>` to `rtl`.

**Adding or editing copy**: edit the English source file (`src/content/en.js`) for structure and English text; update the corresponding key in `ms.js`, `zh.js`, and `ar.js` for the other locales. Rebuild (`npm run build`) to regenerate all prerendered pages.

**RTL support**: `src/styles.css` includes `[dir="rtl"]` overrides for layout properties (flex-direction, text-align, padding/margin asymmetries) so the full page mirrors correctly in Arabic without per-component changes.

## Deploy to Cloudflare Workers (Git integration)

The site deploys via **Workers Builds**: on every push to `main`, Cloudflare runs the build
command, then the deploy command, using [`wrangler.jsonc`](wrangler.jsonc). No secrets live in the
repo. One-time setup in the Cloudflare dashboard (**Workers & Pages → Create → Workers → Import a
repository**, or your existing Worker → **Settings → Build**):

| Setting | Value |
| --- | --- |
| Git repository | `mysayasan/kopiv2` |
| Production branch | `main` |
| **Root directory** | `r450k` |
| Build command | `npm run build` |
| Deploy command | `npx wrangler deploy` |

That's it — there is **no "Build output directory"** field for Workers; the output dir is declared
in `wrangler.jsonc` (`assets.directory: ./dist`). `npm run build` produces `dist/`; `npx wrangler
deploy` uploads the Worker (`worker/index.js`) plus the `dist/` assets.

> **Node 22+ required.** Wrangler 4 needs Node ≥ 22. [`.nvmrc`](.nvmrc) pins `22`, which Cloudflare
> reads automatically; if a build still uses an older Node, add a build variable `NODE_VERSION=22`
> in the Worker's build settings.

> **Why a Worker and not Pages?** Cloudflare now funnels new Git projects into Workers Builds (the
> screen with a *Deploy command* + *Build token*). This site is configured for that model:
> `wrangler.jsonc` serves `dist/` via the static-assets `ASSETS` binding and routes `POST
> /api/contact` to the Worker. Do **not** pick the `Vite` framework preset anywhere — it injects
> `@cloudflare/vite-plugin` (a full-stack Workers integration) which this static site doesn't use
> and which fails the build (`'node:module' does not provide an export named 'registerHooks'`).
> Here the build stays a plain `vite build`; deployment is the separate `wrangler deploy` step.

After the first deploy, every push to `main` rebuilds and publishes automatically; other branches
get preview URLs. You can also deploy by hand from `r450k/`:

```bash
npm run build
npm run deploy        # = npx wrangler deploy (needs `wrangler login` once)
```

> The root `.github/workflows/main.yml` (version-manifest job) is unaffected — Cloudflare builds
> independently of GitHub Actions.

## Custom domain — r450k.com

The site is wired to **`https://r450k.com`**: the canonical link + Open Graph/Twitter tags
in [`index.html`](index.html), [`public/robots.txt`](public/robots.txt),
[`public/sitemap.xml`](public/sitemap.xml), and the `SITE_URL` constant in
[`src/i18n/index.jsx`](src/i18n/index.jsx). To change the domain, update those (search for `r450k.com`).

Connect the domain in Cloudflare (the same domain can host the apex and `www`):

1. **Worker → Settings → Domains & Routes → Add → Custom domain.**
2. Add `r450k.com` (and optionally `www.r450k.com`).
3. If the domain's DNS is already on Cloudflare, the record is added for you. Otherwise point the
   host at the target shown in the dialog and finish the TLS step.
4. Cloudflare issues the certificate automatically; the domain goes live within minutes.

To make `www` and the apex agree, add a redirect (Cloudflare **Bulk Redirects** or a
**Redirect Rule**) from `www.r450k.com` → `r450k.com` (or vice-versa) so there's one canonical host.

## Screenshots

The **Showcase** section displays 7 real mymatasan UI captures from
[`public/screenshots/`](public/screenshots/) (`live_views.png`, `ai_detection.png`,
`teach.png`, `recordings.png`, `dashboard.png`, `backup_recovery.png`,
`version_health_language_selection.png`). Any image that fails to load falls back to its SVG
mockup, so the section never breaks. To replace or add captures, see the instructions and
`sharp` resize command in [`public/screenshots/README.md`](public/screenshots/README.md).

### Social share image

[`public/og-image.svg`](public/og-image.svg) is the Open Graph image. Some social scrapers
(Facebook, some Twitter paths) don't render SVG — for best compatibility, export a **1200×630 PNG**
to `public/og-image.png` and point the `og:image` / `twitter:image` tags in `index.html` at it.

## Contact form (Telegram via the Worker)

The floating **Contact** button (bottom-left) opens a popover with a short **message form**. The
form POSTs same-origin to `/api/contact`, which the Worker ([`worker/index.js`](worker/index.js))
handles and relays to your Telegram via the Bot API. Your bot token stays server-side; it is never
in the static bundle.

### 1. Create a bot and get your chat id

1. In Telegram, message **@BotFather** → `/newbot` → follow prompts → copy the **bot token**
   (looks like `123456789:AA…`).
2. Open a chat with your new bot and **send it any message** (bots can't start a conversation, so
   this is required before they can DM you).
3. Get **your** chat id (the human the bot will message — **not** the bot's own id): message
   **@userinfobot** (it replies with your numeric id), or visit
   `https://api.telegram.org/bot<TOKEN>/getUpdates` after messaging your bot and read
   `result[].message.chat.id`.

### 2. Set the secrets on the Worker

Worker → **Settings → Variables and Secrets** → add two **secret** values:

| Name | Value |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | the BotFather token |
| `TELEGRAM_CHAT_ID` | your numeric chat id |

…or from `r450k/` on the CLI:

```bash
npx wrangler secret put TELEGRAM_BOT_TOKEN
npx wrangler secret put TELEGRAM_CHAT_ID
```

Secrets persist across deploys, so set them once (live immediately — no redeploy needed).

To hide the widget entirely, set `enabled: false` in the `contact` block of
[`src/content/en.js`](src/content/en.js).

### Troubleshooting

- **`403 ... the bot can't send messages to the bot`** — `TELEGRAM_CHAT_ID` is set to the bot's own
  id. Set it to **your** numeric chat id (step 1.3) and make sure you've messaged the bot first.
- **`403 ... bot was blocked by the user` / `chat not found`** — open the bot in Telegram and press
  **Start** (or unblock it), then retry.

### Local testing

`npm run dev` (Vite) serves the static site but **not** the Worker, so the form reports the
endpoint as unreachable. To run the Worker + assets together (real `/api/contact`):

```bash
cp .dev.vars.example .dev.vars   # fill in your real token + chat id (gitignored)
npm run build
npm run cf-dev                   # = npx wrangler dev (serves dist/ + worker with .dev.vars)
```

### Hardening (optional)

The Worker already drops bot spam via a hidden **honeypot** field and caps message length. For
heavier abuse, add **Cloudflare Turnstile** (a free CAPTCHA) to the form and verify the token in the
Worker, and/or a per-IP rate limit via a Workers KV namespace.

## Notes

- `public/` files (incl. `_headers`) are copied verbatim into `dist/` by Vite and served by the
  Workers static-assets layer; `_headers` applies its security + cache rules.
- Routing: static files in `dist/` are served directly; the Worker runs for non-asset paths,
  handling `/api/contact` and otherwise delegating to the `ASSETS` binding (SPA fallback via
  `not_found_handling`). There is no `_redirects` catch-all — it would intercept `/api/contact`.
