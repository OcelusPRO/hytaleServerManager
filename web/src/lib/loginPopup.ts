// Hytale login popup. The handle lives outside React so that the popup can
// be closed even after the component that opened it has unmounted (the
// onboarding screen disappears as soon as the login succeeds).

const NAME = "hytale-login";
const WIDTH = 520;
const HEIGHT = 760;

let popup: Window | null = null;

/** Opens (or reuses) the centred popup; must run inside a click handler. */
export function openLoginPopup(url = "about:blank"): Window | null {
  const left = Math.round(window.screenX + (window.outerWidth - WIDTH) / 2);
  const top = Math.round(window.screenY + (window.outerHeight - HEIGHT) / 2);
  popup = window.open(url, NAME, `popup=yes,width=${WIDTH},height=${HEIGHT},left=${left},top=${top}`);
  return popup;
}

/** Points the open popup at url; returns false when there is no popup. */
export function navigateLoginPopup(url: string): boolean {
  if (!popup || popup.closed) return false;
  popup.location.href = url;
  popup.focus();
  return true;
}

export function closeLoginPopup(): void {
  if (popup && !popup.closed) popup.close();
  popup = null;
}

/** Only https pages of hytale.com may be opened from API data. */
export function trustedHytaleUrl(raw?: string): string | null {
  if (!raw) return null;
  try {
    const url = new URL(raw);
    const trusted = url.protocol === "https:" && (url.hostname === "hytale.com" || url.hostname.endsWith(".hytale.com"));
    return trusted ? url.toString() : null;
  } catch {
    return null;
  }
}
