export const BASE = import.meta.env.VITE_API_BASE_PATH ?? "/api";
export const AUTH_BASE =
  import.meta.env.VITE_AUTH_BASE_PATH ?? BASE.replace(/\/api\/?$/, "");

export class APIError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(`API ${status}: ${message}`);
    this.name = "APIError";
    this.status = status;
  }
}

export function withCredentials(init?: RequestInit): RequestInit {
  return {
    ...init,
    credentials: init?.credentials ?? "include",
  };
}

export async function fetchJSON<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, withCredentials(init));
  if (!res.ok) {
    const body = await res.text();
    let message = body;

    try {
      const parsed = JSON.parse(body) as { error?: { message?: string } };
      if (parsed?.error?.message) {
        message = parsed.error.message;
      }
    } catch {
      // Keep raw body when response is not JSON.
    }

    throw new APIError(res.status, message);
  }
  return res.json() as Promise<T>;
}

export async function postJSON<TResponse, TRequest>(
  path: string,
  body: TRequest,
): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

export async function postNoBodyJSON<TResponse>(
  path: string,
): Promise<TResponse> {
  return fetchJSON<TResponse>(path, {
    method: "POST",
  });
}

export async function deleteNoContent(path: string): Promise<void> {
  const res = await fetch(
    `${BASE}${path}`,
    withCredentials({ method: "DELETE" }),
  );
  if (!res.ok) {
    const body = await res.text();
    let message = body;

    try {
      const parsed = JSON.parse(body) as { error?: { message?: string } };
      if (parsed?.error?.message) {
        message = parsed.error.message;
      }
    } catch {
      // Keep raw body when response is not JSON.
    }

    throw new APIError(res.status, message);
  }
}
