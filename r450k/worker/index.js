// Cloudflare Worker entry for the r450k site (Workers + Static Assets model).
//
// Routing: static files in dist/ are served by the ASSETS binding before the
// Worker runs; the Worker is invoked for non-asset paths. We handle
// POST /api/contact here and delegate everything else back to ASSETS (which
// applies the single-page-application not_found_handling for deep links).
//
// Required secrets — set in the Worker → Settings → Variables and Secrets, or
// `npx wrangler secret put TELEGRAM_BOT_TOKEN` / `... TELEGRAM_CHAT_ID`:
//   TELEGRAM_BOT_TOKEN  - from @BotFather
//   TELEGRAM_CHAT_ID    - your numeric Telegram chat id
// For local dev, put the same keys in r450k/.dev.vars and run `npx wrangler dev`.

const json = (obj, status = 200) =>
  new Response(JSON.stringify(obj), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  });

async function handleContact(request, env) {
  if (request.method !== 'POST') return json({ ok: false, error: 'Method not allowed.' }, 405);

  if (!env.TELEGRAM_BOT_TOKEN || !env.TELEGRAM_CHAT_ID) {
    return json({ ok: false, error: 'Contact endpoint is not configured.' }, 500);
  }

  let body;
  try {
    body = await request.json();
  } catch {
    return json({ ok: false, error: 'Invalid request body.' }, 400);
  }

  // Honeypot: real users never fill this hidden field. Pretend success.
  if (body.website) return json({ ok: true });

  const name = String(body.name || '').slice(0, 100).trim();
  const message = String(body.message || '').slice(0, 2000).trim();
  const page = String(body.page || '').slice(0, 300).trim();

  if (!message) return json({ ok: false, error: 'Message is required.' }, 400);

  // Plain-text message (no parse_mode) — nothing to escape, no injection surface.
  const text =
    `📨 New r450k contact\n` +
    `From: ${name || '(anonymous)'}\n` +
    (page ? `Page: ${page}\n` : '') +
    `\n${message}`;

  try {
    const tg = await fetch(`https://api.telegram.org/bot${env.TELEGRAM_BOT_TOKEN}/sendMessage`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({
        chat_id: env.TELEGRAM_CHAT_ID,
        text,
        disable_web_page_preview: true,
      }),
    });
    if (!tg.ok) {
      const detail = await tg.text().catch(() => '');
      return json({ ok: false, error: `Telegram API error (${tg.status}). ${detail}`.trim() }, 502);
    }
    return json({ ok: true });
  } catch {
    return json({ ok: false, error: 'Failed to reach Telegram.' }, 502);
  }
}

// --- Downloads: read the latest MyMataSan release from the GitHub Releases API,
// categorize the assets by platform, and cache the result at the edge so the
// marketing site's Download section always shows the current build without a
// redeploy (and without hammering GitHub's rate limit). ------------------------

const RELEASES_REPO = 'mysayasan/kopiv2';

// categorizeAsset maps a release asset filename to a platform card, or null for
// assets that aren't user downloads (checksums, etc.). Filenames come from
// GoReleaser + the Windows installer job (see .goreleaser.yaml / release.yml).
function categorizeAsset(name) {
  const n = name.toLowerCase();
  const arch = n.includes('arm64') ? 'arm64' : 'x64';
  if (n.endsWith('.exe')) return { os: 'windows', kind: 'installer', arch, label: `Windows Installer (${arch})` };
  if (n.includes('windows') && n.endsWith('.zip')) return { os: 'windows', kind: 'portable', arch, label: `Windows Portable (${arch})` };
  if (n.endsWith('.deb')) return { os: 'linux', kind: 'deb', arch, label: `Debian / Ubuntu (${arch})` };
  if (n.endsWith('.rpm')) return { os: 'linux', kind: 'rpm', arch, label: `Fedora / RHEL (${arch})` };
  if (n.endsWith('.tar.gz')) return { os: 'linux', kind: 'tarball', arch, label: `Linux tarball (${arch})` };
  return null;
}

async function handleDownloads(request, env) {
  const cache = caches.default;
  const cacheKey = new Request(new URL('/api/downloads', request.url).toString(), { method: 'GET' });
  const cached = await cache.match(cacheKey);
  if (cached) return cached;

  const headers = { 'user-agent': 'r450k-site', accept: 'application/vnd.github+json' };
  if (env.GITHUB_TOKEN) headers.authorization = `Bearer ${env.GITHUB_TOKEN}`;

  let payload;
  let status = 200;
  try {
    const gh = await fetch(`https://api.github.com/repos/${RELEASES_REPO}/releases/latest`, { headers });
    if (gh.ok) {
      const rel = await gh.json();
      const version = String(rel.tag_name || '').replace(/^v/, '');
      const assets = (rel.assets || [])
        .map((a) => {
          const cat = categorizeAsset(a.name);
          // Download through the Worker (/api/download/<id>) rather than the raw
          // GitHub URL: for a PRIVATE repo the browser_download_url 404s for
          // anonymous visitors, so the Worker authenticates and redirects to the
          // short-lived signed CDN URL. Works for public repos too.
          return cat ? { ...cat, name: a.name, url: `/api/download/${a.id}`, size: a.size } : null;
        })
        .filter(Boolean);
      payload = {
        ok: true,
        version: rel.tag_name || '',
        publishedAt: rel.published_at || '',
        htmlUrl: rel.html_url || `https://github.com/${RELEASES_REPO}/releases`,
        assets,
        docker: version ? { image: `ghcr.io/mysayasan/mymatasan:${version}` } : null,
      };
    } else {
      status = 502;
      payload = { ok: false, error: `GitHub API ${gh.status}`, htmlUrl: `https://github.com/${RELEASES_REPO}/releases` };
    }
  } catch {
    status = 502;
    payload = { ok: false, error: 'Failed to reach GitHub.', htmlUrl: `https://github.com/${RELEASES_REPO}/releases` };
  }

  const resp = new Response(JSON.stringify(payload), {
    status,
    headers: {
      'content-type': 'application/json; charset=utf-8',
      // 10-minute edge cache so a new release surfaces within minutes, no redeploy.
      'cache-control': payload.ok ? 'public, max-age=600' : 'no-store',
    },
  });
  if (payload.ok) await cache.put(cacheKey, resp.clone());
  return resp;
}

// handleAssetDownload authenticates to GitHub for a release asset (needed for a
// private repo) and streams the bytes back through the Worker as a SAME-ORIGIN
// attachment. We deliberately do NOT 302-redirect the browser to GitHub's signed
// CDN URL: that hop is cross-origin, and a browser navigating to a cross-origin
// download-via-redirect behaves inconsistently (opens a blank tab, or bounces to
// the top of the page, without ever downloading). A same-origin response with
// Content-Disposition: attachment downloads reliably everywhere. The asset id
// comes from /api/downloads. Cloudflare streams the body, so the file is never
// buffered in the Worker. Authorization is dropped automatically when fetch
// follows GitHub's redirect cross-origin to the signed URL (which needs none).
async function handleAssetDownload(request, env, assetId) {
  if (!/^\d+$/.test(assetId)) {
    return new Response('Bad asset id', { status: 400 });
  }
  const headers = { 'user-agent': 'r450k-site', accept: 'application/octet-stream' };
  if (env.GITHUB_TOKEN) headers.authorization = `Bearer ${env.GITHUB_TOKEN}`;
  const gh = await fetch(`https://api.github.com/repos/${RELEASES_REPO}/releases/assets/${assetId}`, {
    headers,
    redirect: 'follow',
  });
  if (!gh.ok || !gh.body) {
    return new Response('Download unavailable', { status: gh.status || 502 });
  }
  const out = new Headers();
  out.set('content-type', gh.headers.get('content-type') || 'application/octet-stream');
  // Preserve GitHub's filename; fall back to a bare attachment if absent.
  out.set('content-disposition', gh.headers.get('content-disposition') || 'attachment');
  const len = gh.headers.get('content-length');
  if (len) out.set('content-length', len);
  return new Response(gh.body, { status: 200, headers: out });
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === '/api/contact') return handleContact(request, env);
    if (url.pathname === '/api/downloads') return handleDownloads(request, env);
    const dl = url.pathname.match(/^\/api\/download\/(\d+)$/);
    if (dl) return handleAssetDownload(request, env, dl[1]);
    return env.ASSETS.fetch(request);
  },
};
