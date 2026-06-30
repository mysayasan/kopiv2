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

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname === '/api/contact') return handleContact(request, env);
    return env.ASSETS.fetch(request);
  },
};
