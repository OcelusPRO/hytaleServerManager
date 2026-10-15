// Passkeys (WebAuthn): converts the JSON options sent by the manager to the
// browser API types and the resulting credentials back to JSON (binary
// fields in base64url, as in WebAuthn Level 3).

/** Passkeys need a secure context: HTTPS, or http://localhost. */
export function passkeysSupported(): boolean {
  return window.isSecureContext && typeof window.PublicKeyCredential === "function";
}

function fromBase64url(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, "+").replace(/_/g, "/").padEnd(Math.ceil(value.length / 4) * 4, "=");
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  return bytes.buffer;
}

function toBase64url(buffer: ArrayBuffer | null | undefined): string {
  if (!buffer) return "";
  let binary = "";
  for (const b of new Uint8Array(buffer)) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

interface JSONDescriptor {
  type: "public-key";
  id: string;
  transports?: AuthenticatorTransport[];
}

function descriptors(list: JSONDescriptor[] | undefined): PublicKeyCredentialDescriptor[] | undefined {
  return list?.map((d) => ({ type: d.type, id: fromBase64url(d.id), transports: d.transports }));
}

export interface CeremonyOptions {
  ceremony: string;
  publicKey: Record<string, unknown>;
}

/** Runs navigator.credentials.create and returns the response to send back. */
export async function createPasskey(options: CeremonyOptions, name: string) {
  const pk = options.publicKey as {
    challenge: string;
    user: { id: string; name: string; displayName: string };
    excludeCredentials?: JSONDescriptor[];
  } & Omit<PublicKeyCredentialCreationOptions, "challenge" | "user" | "excludeCredentials">;
  const credential = (await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: fromBase64url(pk.challenge),
      user: { ...pk.user, id: fromBase64url(pk.user.id) },
      excludeCredentials: descriptors(pk.excludeCredentials),
    },
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error("cancelled");
  const response = credential.response as AuthenticatorAttestationResponse;
  return {
    ceremony: options.ceremony,
    name,
    credential: credentialJSON(credential, {
      clientDataJSON: toBase64url(response.clientDataJSON),
      attestationObject: toBase64url(response.attestationObject),
      transports: response.getTransports?.() ?? [],
    }),
  };
}

/** The PublicKeyCredential in its standard JSON form (WebAuthn Level 3). */
function credentialJSON(credential: PublicKeyCredential, response: Record<string, unknown>) {
  return {
    id: credential.id,
    rawId: toBase64url(credential.rawId),
    type: credential.type,
    authenticatorAttachment: credential.authenticatorAttachment ?? undefined,
    clientExtensionResults: credential.getClientExtensionResults(),
    response,
  };
}

/** Runs navigator.credentials.get and returns the response to send back. */
export async function getPasskey(options: CeremonyOptions) {
  const pk = options.publicKey as { challenge: string; allowCredentials?: JSONDescriptor[] } & Omit<
    PublicKeyCredentialRequestOptions,
    "challenge" | "allowCredentials"
  >;
  const credential = (await navigator.credentials.get({
    publicKey: { ...pk, challenge: fromBase64url(pk.challenge), allowCredentials: descriptors(pk.allowCredentials) },
  })) as PublicKeyCredential | null;
  if (!credential) throw new Error("cancelled");
  const response = credential.response as AuthenticatorAssertionResponse;
  return {
    ceremony: options.ceremony,
    credential: credentialJSON(credential, {
      clientDataJSON: toBase64url(response.clientDataJSON),
      authenticatorData: toBase64url(response.authenticatorData),
      signature: toBase64url(response.signature),
      userHandle: response.userHandle ? toBase64url(response.userHandle) : undefined,
    }),
  };
}

/** True when the user dismissed the browser's passkey dialog. */
export function isCancellation(err: unknown): boolean {
  return err instanceof DOMException && (err.name === "NotAllowedError" || err.name === "AbortError");
}
