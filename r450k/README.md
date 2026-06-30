# r450k — product site

Standalone static marketing site for the **r450k** platform, built with React + Vite and
deployed to **Cloudflare Pages**. It is decoupled from the Go backend; it ships nothing but a
static `dist/` bundle.

Content is sourced from the platform READMEs (primarily [`apps/mymatasan/README.md`](../apps/mymatasan/README.md))
and lives entirely in [`src/content.js`](src/content.js) — edit copy there without touching layout.

## Local development

```bash
cd r450k
npm install
npm run dev      # http://localhost:5173
npm run build    # outputs static site to dist/
npm run preview  # serve the built dist/ locally
```

Requires Node 20+ (see [`.nvmrc`](.nvmrc)).

## Project layout

```
r450k/
  index.html            # entry HTML + meta tags
  vite.config.js
  public/
    favicon.svg
    _headers            # Cloudflare Pages security + cache headers
    _redirects          # SPA fallback to index.html
  src/
    main.jsx
    App.jsx
    content.js          # ALL site copy — edit here
    styles.css          # dark theme, mirrors the apps' --theme-dark palette
    components/         # Logo, Icon
    sections/           # Nav, Hero, Features, HowItWorks, UseCases, Apps, FinalCta, Footer
```

## Deploy to Cloudflare Pages (Git integration)

Deploys automatically on every push to `main` — no secrets in the repo, no GitHub Actions.
One-time setup in the Cloudflare dashboard:

1. **Cloudflare dashboard → Workers & Pages → Create → Pages → Connect to Git.**
2. Authorize GitHub and select the `mysayasan/kopiv2` repository.
3. Configure the build:
   | Setting | Value |
   | --- | --- |
   | Production branch | `main` |
   | **Framework preset** | **`None`** |
   | **Root directory** | `r450k` |
   | Build command | `npm run build` |
   | Build output directory | `dist` |
4. (Optional) Environment variable `NODE_VERSION = 20`.
5. **Save and Deploy.**

> **Use `None`, not the `Vite` preset.** Cloudflare's `Vite` framework preset injects
> `@cloudflare/vite-plugin` (its full-stack *Workers* integration) into the build. This site is a
> plain static SPA + Pages Functions, so that plugin is wrong for it and also fails on the build
> image's Node version (`SyntaxError: ... 'node:module' does not provide an export named
> 'registerHooks'`). With preset `None`, Pages just runs `npm run build` (plain `vite build`) and
> serves `dist/`, and the `functions/` directory is compiled by Pages as usual.

After the first deploy, every push to `main` that touches `r450k/` rebuilds and publishes
automatically. Pull requests get preview deployments at unique URLs.

> The root `.github/workflows/main.yml` (version-manifest job) is unaffected — Cloudflare Pages
> builds independently of GitHub Actions.

## Custom domain — r450k.com

The site is wired to **`https://r450k.com`**: the canonical link + Open Graph/Twitter tags
in [`index.html`](index.html), [`public/robots.txt`](public/robots.txt),
[`public/sitemap.xml`](public/sitemap.xml), and the `siteUrl` constant in
[`src/content.js`](src/content.js). To change the domain, update those (search for `r450k.com`).

Connect the domain in Cloudflare (the same domain can host the apex and `www`):

1. **Pages project → Custom domains → Set up a custom domain.**
2. Add `r450k.com` (and optionally `www.r450k.com`).
3. If the domain's DNS is already on Cloudflare, the `CNAME`/flattened record is added for you.
   Otherwise, point the host at the Pages target shown in the dialog and finish the TLS step.
4. Cloudflare issues the certificate automatically; the domain goes live within minutes.

To make `www` and the apex agree, add a redirect (Cloudflare **Bulk Redirects** or a
**Redirect Rule**) from `www.r450k.com` → `r450k.com` (or vice-versa) so there's one canonical host.

## Screenshots

The **Showcase** section renders styled SVG mockups of the mymatasan UI by default. To use real
captures, drop PNG/WebP files into [`public/screenshots/`](public/screenshots/) and uncomment the
matching `src:` lines in `showcase.shots` (see that folder's README). Any image that fails to load
falls back to its mockup, so the section never breaks.

### Social share image

[`public/og-image.svg`](public/og-image.svg) is the Open Graph image. Some social scrapers
(Facebook, some Twitter paths) don't render SVG — for best compatibility, export a **1200×630 PNG**
to `public/og-image.png` and point the `og:image` / `twitter:image` tags in `index.html` at it.

## Contact form (Telegram via a Cloudflare Worker)

The floating **Contact** button (bottom-left) opens a popover with a one-tap **Open Telegram**
deep link *and* a short **message form**. The form POSTs to a **Pages Function** — a Cloudflare
Worker that runs on the same domain at `/api/contact` ([`functions/api/contact.js`](functions/api/contact.js))
— which relays the message to your Telegram via the Bot API. Your bot token stays server-side; it
is never in the static bundle.

### 1. Create a bot and get your chat id

1. In Telegram, message **@BotFather** → `/newbot` → follow prompts → copy the **bot token**
   (looks like `123456789:AA…`).
2. Open a chat with your new bot and send it any message (so it can DM you back).
3. Get your **chat id**: message **@userinfobot** (it replies with your numeric id), or visit
   `https://api.telegram.org/bot<TOKEN>/getUpdates` after messaging your bot and read
   `result[].message.chat.id`.

### 2. Set the secrets in Cloudflare Pages

Pages project → **Settings → Environment variables** → add two **encrypted** variables (Production
*and* Preview if you want previews to work):

| Name | Value |
| --- | --- |
| `TELEGRAM_BOT_TOKEN` | the BotFather token |
| `TELEGRAM_CHAT_ID` | your numeric chat id |

Redeploy (or push) so the Function picks them up. No `wrangler.toml` is needed — Pages
auto-detects the `functions/` directory at the project root.

### 3. Set your handle

Edit `telegramHandle` in [`src/content.js`](src/content.js) (the `contact` block) to your Telegram
username for the deep-link button. Set `enabled: false` there to hide the widget entirely.

### Local testing

`npm run dev` (Vite) serves the static site but **not** the Function, so the form will report the
endpoint as unreachable — the Telegram deep link still works. To test the Function locally:

```bash
cp .dev.vars.example .dev.vars   # fill in your real token + chat id (gitignored)
npm run build
npx wrangler pages dev dist      # serves dist/ + functions/ with .dev.vars
```

### Standalone Worker alternative

If you'd rather run a separate Worker (e.g. shared across sites), deploy the same logic as its own
Worker and point `contact.endpoint` in `src/content.js` at its full URL. The Worker must then send
CORS headers (`Access-Control-Allow-Origin: https://r450k.com`) and handle an `OPTIONS` preflight —
neither is needed for the same-origin Pages Function above, which is why it's the default.

### Hardening (optional)

The Function already drops bot spam via a hidden **honeypot** field and caps message length. For
heavier abuse, add **Cloudflare Turnstile** (a free CAPTCHA) to the form and verify the token in the
Function, and/or a per-IP rate limit via a Workers KV namespace.

## Notes

- `_headers` / `_redirects` in `public/` are copied verbatim into `dist/` by Vite and read by
  Cloudflare Pages.
- The static site is served from `dist/`; the `functions/` directory is deployed by Pages as
  serverless Workers (only `/api/contact` is used today).
