import { initData } from './tg'

export class ApiError extends Error {
  constructor(public status: number, public code: string) { super(code) }
}

// e2e only: ?dev=<id>:<name>&devtoken=<token>; the server ignores it unless DEV_TOKEN is configured
const q = new URLSearchParams(location.search)
if (q.get('dev')) { sessionStorage.setItem('vd-dev', q.get('dev')!); sessionStorage.setItem('vd-devtoken', q.get('devtoken') || '') }
const devUser = () => { try { return sessionStorage.getItem('vd-dev') } catch { return null } }

export async function api<T = any>(method: string, path: string, body?: unknown): Promise<T> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  const d = devUser()
  if (d) { const [id, name] = d.split(':'); h['X-Dev-User'] = id; h['X-Dev-Name'] = encodeURIComponent(name || 'Dev'); h['X-Dev-Token'] = sessionStorage.getItem('vd-devtoken') || '' }
  else if (initData()) h['Authorization'] = 'tma ' + initData() // outside Telegram the session cookie is used
  const res = await fetch('/api' + path, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error || 'error')
  return data as T
}

export const get = <T = any>(p: string) => api<T>('GET', p)
export const post = <T = any>(p: string, b?: unknown) => api<T>('POST', p, b ?? {})
export const patch = <T = any>(p: string, b: unknown) => api<T>('PATCH', p, b)
export const del = <T = any>(p: string) => api<T>('DELETE', p)

export const errText: Record<string, string> = {
  conflict: 'Партнёр как раз ответил — обновил экран.',
  closed: 'Этот выбор уже закрыт.',
  wrong_state: 'Этот шаг уже пройден.',
  not_paired: 'Сначала пригласи партнёра.',
  round_open: 'У вас уже идёт выбор — сначала завершите его.',
  no_ideas: 'В копилке нечего предложить.',
  limit: 'На сегодня вопросов достаточно — продолжим завтра.',
  unauthorized: 'Открой приложение из Telegram.',
}
export const humanError = (e: unknown) => (e instanceof ApiError && errText[e.code]) || 'Не получилось. Попробуй ещё раз.'
