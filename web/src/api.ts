export class SignedOut extends Error {}
export class APIError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}
export function userMessage(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    if (/sunday/i.test(error.message)) return 'Light controls are unavailable on Sundays for now.';
    if (error.status === 409)
      return 'Someone else saved a change. Your edits are still here; load the latest version or save a copy.';
    if (error.status === 413) return 'This file is too large. Choose a smaller file.';
    if (error.status === 400 && /latest controller snapshot/.test(error.message))
      return 'This saved-on-lights pattern no longer matches your lights, so it cannot be added as a copy.';
    if (error.status === 400) return 'Check your entries and try again.';
    if (error.status === 403) return 'Please sign in again to continue.';
    return fallback;
  }
  if (error instanceof TypeError || error instanceof SyntaxError) return fallback;
  return error instanceof Error ? error.message : fallback;
}
export async function request<T>(
  path: string,
  csrf?: string,
  body?: unknown,
  options?: { method?: string; revision?: number },
): Promise<T> {
  const headers: Record<string, string> = csrf
    ? { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf }
    : {};
  if (options?.revision !== undefined) headers['If-Match'] = `"${options.revision}"`;
  const response = await fetch(path, {
    credentials: 'same-origin',
    cache: 'no-store',
    method: options?.method ?? (csrf ? 'POST' : 'GET'),
    headers,
    body: csrf ? JSON.stringify(body ?? {}) : undefined,
  });
  if (response.status === 401) throw new SignedOut();
  if (!response.ok) {
    let message = `Request failed (${response.status})`;
    try {
      const value = await response.json();
      if (typeof value.error === 'string') message = value.error;
    } catch {
      /* Do not display an HTML gateway error as application content. */
    }
    throw new APIError(message, response.status);
  }
  return response.status === 204 ? (undefined as T) : response.json();
}
