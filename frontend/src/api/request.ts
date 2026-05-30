export const BASE = import.meta.env.VITE_API_BASE_PATH ?? "/api";
export const AUTH_BASE =
  import.meta.env.VITE_AUTH_BASE_PATH ?? BASE.replace(/\/api\/?$/, "");

export class APIError extends Error {
  status: number;
  code?: string;

  constructor(status: number, message: string, code?: string) {
    super(`API ${status}: ${message}`);
    this.name = "APIError";
    this.status = status;
    this.code = code;
  }
}

function parseAPIErrorBody(body: string): { message: string; code?: string } {
  let message = body;
  let code: string | undefined;

  try {
    const parsed = JSON.parse(body) as {
      error?: { message?: string; code?: string };
    };
    if (parsed?.error?.message) {
      message = parsed.error.message;
    }
    if (parsed?.error?.code) {
      code = parsed.error.code;
    }
  } catch {
    // Keep raw body when response is not JSON.
  }

  return { message, code };
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
    const { message, code } = parseAPIErrorBody(body);

    throw new APIError(res.status, message, code);
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
    const { message, code } = parseAPIErrorBody(body);

    throw new APIError(res.status, message, code);
  }
}
