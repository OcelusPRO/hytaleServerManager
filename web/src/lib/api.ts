import { translate, translateServer } from "./i18n";

// HTTP client of the manager API. Every mutating request carries the
// anti-CSRF header expected by the server.

export class ApiError extends Error {
  readonly status: number;

  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const CSRF_HEADER = { "X-Requested-With": "hsm" } as const;
const UNAUTHORIZED_EVENT = "hsm:unauthorized";

/** Subscribes to session expiry notifications. */
export function onUnauthorized(handler: () => void): () => void {
  window.addEventListener(UNAUTHORIZED_EVENT, handler);
  return () => window.removeEventListener(UNAUTHORIZED_EVENT, handler);
}

function notifyUnauthorized(url: string) {
  if (url !== "/api/login") window.dispatchEvent(new Event(UNAUTHORIZED_EVENT));
}

async function request<T>(method: string, url: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { ...CSRF_HEADER };
  let payload: string | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const response = await fetch(url, { method, headers, body: payload, credentials: "same-origin" });
  if (response.status === 401) notifyUnauthorized(url);
  if (response.status === 204) return undefined as T;

  const isJSON = response.headers.get("Content-Type")?.includes("application/json");
  const data: unknown = isJSON ? await response.json() : null;
  if (!response.ok) {
    const message = (data as { error?: string } | null)?.error ?? translate("common.httpError", { status: response.status });
    throw new ApiError(response.status, message);
  }
  return data as T;
}

/** Uploads a file with progress reporting (fetch cannot report upload progress). */
function upload<T>(url: string, file: File, onProgress?: (fraction: number) => void): Promise<T> {
  return new Promise((resolve, reject) => {
    const form = new FormData();
    form.append("file", file, file.name);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", url);
    xhr.setRequestHeader("X-Requested-With", CSRF_HEADER["X-Requested-With"]);
    xhr.upload.addEventListener("progress", (event) => {
      if (event.lengthComputable) onProgress?.(event.loaded / event.total);
    });
    xhr.addEventListener("load", () => {
      let data: unknown = null;
      try {
        data = xhr.responseText ? JSON.parse(xhr.responseText) : null;
      } catch {
        // not JSON
      }
      if (xhr.status === 401) notifyUnauthorized(url);
      if (xhr.status >= 200 && xhr.status < 300) resolve(data as T);
      else reject(new ApiError(xhr.status, (data as { error?: string } | null)?.error ?? translate("common.httpError", { status: xhr.status })));
    });
    xhr.addEventListener("error", () => reject(new ApiError(0, translate("common.networkError"))));
    xhr.send(form);
  });
}

export const api = {
  get: <T>(url: string) => request<T>("GET", url),
  post: <T = void>(url: string, body?: unknown) => request<T>("POST", url, body),
  put: <T = void>(url: string, body?: unknown) => request<T>("PUT", url, body),
  patch: <T = void>(url: string, body?: unknown) => request<T>("PATCH", url, body),
  del: (url: string) => request<void>("DELETE", url),
  upload,
};

/** Encodes a value for use as a URL path segment. */
export const seg = encodeURIComponent;

/** Message of a failure, translated when it comes from the server. */
export function errorMessage(err: unknown): string {
  return translateServer(err instanceof Error ? err.message : String(err));
}
