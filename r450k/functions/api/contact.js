// Cloudflare Pages Function — POST /api/contact
// Relays a contact-form submission to a Telegram chat via the Bot API.
// Runs same-origin with the static site, so no CORS is needed.
//
// Required environment variables (set in the Pages project → Settings →
// Environment variables, as ENCRYPTED secrets — never commit them):
//   TELEGRAM_BOT_TOKEN  - from @BotFather (e.g. 1234:AAbb...)
//   TELEGRAM_CHAT_ID    - your numeric chat id (DM the bot, then read getUpdates,
//                         or message @userinfobot to find it)
//
// For local testing, put the same keys in r450k/.dev.vars (gitignored) and run
// `npx wrangler pages dev dist`. See README → "Contact form".

const json = (obj, status = 200) =>
  new Response(JSON.stringify(obj), {
    status,
    headers: { 'content-type': 'application/json; charset=utf-8' },
  });

export async function onRequestPost(context) {
  const { request, env } = context;

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
  } catch (err) {
    return json({ ok: false, error: 'Failed to reach Telegram.' }, 502);
  }
}

// Anything other than POST → 405.
export async function onRequest(context) {
  if (context.request.method === 'POST') return onRequestPost(context);
  return json({ ok: false, error: 'Method not allowed.' }, 405);
}
