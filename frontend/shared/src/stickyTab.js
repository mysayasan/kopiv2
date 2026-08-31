import { useCallback, useEffect, useState } from 'react';

// The section an operator is looking at is part of where they are, and a refresh should not
// move them. Every app in the suite held it in a plain useState seeded with 'dashboard', so
// F5 — or a browser restoring the tab, or a crash — silently threw the operator back to the
// landing page. On an appliance that somebody watches all day, that is the single most-hit
// piece of state in the product.
//
// WHY sessionStorage AND NOT A COOKIE OR localStorage:
//   - It is per TAB. Two tabs open on Cameras and Timeline each keep their own place; with a
//     cookie or localStorage the second tab to load adopts whatever the first one last wrote,
//     so opening a new tab quietly moves the old one's saved position.
//   - It is never sent to the server. This is UI state no handler reads, and mymatasan issues
//     a very large number of media requests per minute — a cookie rides on every one of them.
//   - It dies with the tab, so a brand-new window opens on the landing page, which is what
//     somebody opening a new window is asking for.
// It survives reload, restore-after-crash, and back/forward — which is the whole ask.
//
// MYIDSAN IS THE ONE APP THAT DOES NOT USE THIS. It solved the same problem first, with a
// cookie (`myidsan_active_section`) and a dedicated UnauthorizedPage for a restored section the
// signed-in user may not have — which is a better answer than the silent demotion here, because
// it tells the operator why they moved instead of just moving them. It is left alone rather than
// rewritten: it works, and its navigation is wired into the permission matrix. If the two are
// ever unified, adopt myidsan's explain-don't-demote behaviour, not the other direction.
//
// WHAT IT DELIBERATELY IS NOT: a router. The address bar does not change, so a section is not
// linkable or bookmarkable and the back button still leaves the app. Making sections real URLs
// is a bigger, better change; this is the part that fixes the refresh.

// readStickyTab returns the stored section for `key`, or '' when there is none. Storage access
// is wrapped because sessionStorage THROWS rather than returning null when the browser is
// configured to block storage (Safari private mode, an embedded webview, a locked-down kiosk
// profile) — and an app that will not boot without a place to remember a tab name is worse
// than one that simply forgets.
export function readStickyTab(key) {
  try {
    return window.sessionStorage.getItem(key) || '';
  } catch (_) {
    return '';
  }
}

export function writeStickyTab(key, value) {
  try {
    if (value) window.sessionStorage.setItem(key, value);
    else window.sessionStorage.removeItem(key);
  } catch (_) {
    /* storage blocked — the tab simply will not be remembered */
  }
}

// clearStickyTab drops the remembered section. Call it on SIGN-OUT. Two reasons, and the
// second is the one that matters: the next person to sign in on this machine should start at
// the landing page rather than inside the last operator's work, and where an administrator was
// working is itself a small disclosure — "the last user of this terminal was in Audit" is not
// something a shift handover should leak to whoever sits down next.
export function clearStickyTab(key) {
  writeStickyTab(key, '');
}

// useStickyTab is useState for the active section, backed by sessionStorage.
//
// `allowed` is the set of sections this user can currently reach — an array or a Set. It is
// the load-bearing argument. A remembered section can go stale in ways that matter:
//   - the section was renamed or removed by an upgrade, leaving a name nothing renders;
//   - an admin was on Settings, signed out, and a VIEWER signed in on the same tab. The nav
//     would correctly hide Settings while the restored state still rendered it. Every app
//     here gates its screens on the server too, so this is not the only line of defence — but
//     a screen that draws admin chrome and then fails every request is not a defence, it is a
//     bug report waiting to be filed.
// Pass a falsy `allowed` to skip the check entirely (e.g. while permissions are still
// loading), which is NOT the same as passing an empty list — an empty list means "this user
// may reach nothing", and would bounce them to the fallback.
export function useStickyTab(key, fallback, allowed) {
  const [tab, setTabState] = useState(() => readStickyTab(key) || fallback);

  const setTab = useCallback((next) => {
    if (!next) return;
    setTabState(next);
    writeStickyTab(key, next);
  }, [key]);

  // Membership is computed as a primitive, not an object, so the effect below re-runs when the
  // ANSWER changes rather than every time the caller happens to build a new array.
  const permitted = !allowed || (Array.isArray(allowed) ? allowed.includes(tab) : allowed.has(tab));

  useEffect(() => {
    // Rewrite storage as well as state: a stale name left in storage would bounce the operator
    // again on the next reload, and they would have no way to see why.
    if (!permitted) setTab(fallback);
  }, [permitted, fallback, setTab]);

  // While a disallowed section is being corrected, report the fallback rather than the section
  // the user may not have. Without this the offending screen renders for one frame.
  return [permitted ? tab : fallback, setTab];
}
