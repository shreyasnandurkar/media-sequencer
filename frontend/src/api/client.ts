export type MediaType = 'image' | 'video' | 'blank';

export interface Media {
  id: string;
  name: string;
  type: MediaType;
  url: string | null;
  durationMs: number;
}

export interface PlaylistItem {
  id: number;
  mediaId: string;
  position: number;
  durationMs: number;
}

export interface WindowState {
  id: string;
  name: string;
  cycleEpoch: number;
  anchorAt: number | null;
  anchorIndex: number | null;
  version: number;
  position: number;
  items: PlaylistItem[];
}

export interface SyncState {
  id: number;
  mediaId: string;
  startAt: number;
  endAt: number;
}

export interface AppState {
  serverTimeMs: number;
  cycleMs: number;
  media: Media[];
  windows: WindowState[];
  activeSync: SyncState | null;
}

export const API_BASE_URL: string =
  import.meta.env.VITE_API_BASE_URL ?? 'http://localhost:8080';

interface ApiErrorBody {
  error?: { code?: string; message?: string };
}

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json', ...init?.headers } : init?.headers,
    cache: 'no-store',
  });

  if (!res.ok) {
    let code = 'http_error';
    let message = `${res.status} ${res.statusText}`;
    try {
      const body = (await res.json()) as ApiErrorBody;
      if (body.error?.message) {
        code = body.error.code ?? code;
        message = body.error.message;
      }
    } catch {

    }
    throw new ApiError(res.status, code, message);
  }

  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  health: () => request<{ status: string }>('/api/health'),

  state: () => request<AppState>('/api/state'),

  createMedia: (body: {
    name: string;
    type: MediaType;
    url?: string | null;
    durationMs: number;
  }) => request<Media>('/api/media', { method: 'POST', body: JSON.stringify(body) }),

  addItem: (windowId: string, body: { mediaId: string; position?: number; durationMs?: number }) =>
    request<WindowState>(`/api/windows/${encodeURIComponent(windowId)}/items`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  deleteItem: (windowId: string, itemId: number) =>
    request<WindowState>(
      `/api/windows/${encodeURIComponent(windowId)}/items/${itemId}`,
      { method: 'DELETE' },
    ),

  reorderItems: (windowId: string, itemIds: number[]) =>
    request<WindowState>(`/api/windows/${encodeURIComponent(windowId)}/items/order`, {
      method: 'PUT',
      body: JSON.stringify({ itemIds }),
    }),

  startSync: (body: { mediaId: string; durationMs?: number }) =>
    request<SyncState>('/api/sync', { method: 'POST', body: JSON.stringify(body) }),

  cancelSync: () => request<{ cancelled: boolean }>('/api/sync/active', { method: 'DELETE' }),
};

export const eventsUrl = `${API_BASE_URL}/api/events`;
