export class APIError extends Error {
  constructor(public status: number, message: string) { super(message) }
}
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch('/api' + path, {
    ...options, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-Flowdesk-Request': '1', ...options.headers },
  })
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: 'Сервер временно недоступен' }))
    throw new APIError(response.status, body.error || 'Не удалось выполнить запрос')
  }
  if (response.status === 204) return undefined as T
  return response.json()
}
export function errorText(error: unknown): string {
  return error instanceof Error ? error.message : 'Не удалось выполнить действие'
}
