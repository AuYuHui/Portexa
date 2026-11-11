import { t } from "./i18n";
let csrfToken = "";
let expired: () => void = () => undefined;

export class APIError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}
export function configureSession(csrf: string, onExpired: () => void) {
  csrfToken = csrf;
  expired = onExpired;
}
export async function api<T>(
  path: string,
  method = "GET",
  data?: unknown,
  signal?: AbortSignal,
): Promise<T> {
  const headers: Record<string, string> = { Accept: "application/json" };
  if (method !== "GET") {
    headers["Content-Type"] = "application/json";
    if (csrfToken) headers["X-CSRF-Token"] = csrfToken;
  }
  const response = await fetch(`/api${path}`, {
    method,
    headers,
    credentials: "same-origin",
    cache: "no-store",
    signal,
    body: data === undefined ? undefined : JSON.stringify(data),
  });
  const result = await response.json().catch(() => ({}));
  if (!response.ok) {
    if (response.status === 401) expired();
    throw new APIError(
      response.status,
      result.error || t('api.serviceTemporarilyUnavailablePleaseRetryLater'),
    );
  }
  return result as T;
}
