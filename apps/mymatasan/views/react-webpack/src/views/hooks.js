import React from 'react';
import { apiBase } from './lib/helpers';

export function useSnapshotBlob(alertId, authHeader) {
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
    fetch(`${apiBase()}/api/vision/alerts/${alertId}/snapshot`, { credentials: 'include', headers })
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
  }, [alertId, authHeader]);
  return { url, loading, error };
}

