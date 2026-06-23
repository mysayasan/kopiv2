import React from 'react';
import { apiBase } from './lib/helpers';

// useSnapshotBlob fetches an alert's snapshot as an object URL. Pass
// annotated=true to get the server-drawn version (detection boxes + labels), used
// by the notification row thumbnails so the event screenshot shows what fired.
export function useSnapshotBlob(alertId, authHeader, annotated = false) {
  const [url, setUrl] = React.useState(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState(false);
  React.useEffect(() => {
    if (!alertId) { setUrl(null); setError(false); return; }
    let revoked = false;
    let objectUrl = null;
    setLoading(true);
    setError(false);
    setUrl(null);
    const headers = authHeader ? { Authorization: authHeader } : {};
    const suffix = annotated ? '?annotated=1' : '';
    fetch(`${apiBase()}/api/vision/alerts/${alertId}/snapshot${suffix}`, { credentials: 'include', headers })
      .then((r) => { if (!r.ok) throw new Error(r.status); return r.blob(); })
      .then((blob) => {
        if (revoked) return;
        objectUrl = URL.createObjectURL(blob);
        setUrl(objectUrl);
      })
      .catch(() => { if (!revoked) setError(true); })
      .finally(() => { if (!revoked) setLoading(false); });
    return () => {
      revoked = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [alertId, authHeader, annotated]);
  return { url, loading, error };
}

