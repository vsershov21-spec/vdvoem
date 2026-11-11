import { initData } from './tg'

export class ApiError extends Error {
  constructor(public status: number, public code: string) { super(code) }
}

// e2e only: ?dev=<id>:<name>&devtoken=<token>; the server ignores it unless DEV_TOKEN is configured
const q = new URLSearchParams(location.search)
if (q.get('dev')) { sessionStorage.setItem('vd-dev', q.get('dev')!); sessionStorage.setItem('vd-devtoken', q.get('devtoken') || '') }
const devUser = () => { try { return sessionStorage.getItem('vd-dev') } catch { return null } }

// one id per open tab: the live stream skips echoes of this tab's own changes
export const CLIENT_ID = Math.random().toString(36).slice(2, 12)

export function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'X-Client': CLIENT_ID }
  const d = devUser()
  if (d) { const [id, name] = d.split(':'); h['X-Dev-User'] = id; h['X-Dev-Name'] = encodeURIComponent(name || 'Dev'); h['X-Dev-Token'] = sessionStorage.getItem('vd-devtoken') || '' }
  else if (initData()) h['Authorization'] = 'tma ' + initData() // outside Telegram the session cookie is used
  return h
}

// a request the person waited for over 1.5 s is reported with its timing: the owner sees where the phone loses time
// (network before the server answered, or the server itself — its own time is in the response header)
const slow = (method: string, path: string, ms: number, res?: Response) => {
  if (ms < 1500) return
  const srv = res?.headers.get('X-Server-Ms') || '?'
  ;(window as any).__vdlog?.('slow', `${method} ${path.split('?')[0]} ${ms}ms server ${srv}ms`)
}

export async function api<T = any>(method: string, path: string, body?: unknown): Promise<T> {
  const h = { ...authHeaders(), 'Content-Type': 'application/json' }
  const t0 = performance.now()
  let res: Response
  try { res = await fetch('/api' + path, { method, headers: h, body: body === undefined ? undefined : JSON.stringify(body) }) }
  catch (e) { slow(method, path, Math.round(performance.now() - t0)); throw e }
  if (!(method === 'POST' && path.startsWith('/assistant'))) slow(method, path, Math.round(performance.now() - t0), res) // the assistant thinks: its time is its own
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error || 'error')
  return data as T
}

// a file upload (a voice recording) — multipart, same auth
export async function upload<T = any>(path: string, form: FormData): Promise<T> {
  const res = await fetch('/api' + path, { method: 'POST', headers: authHeaders(), body: form })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, data.error || 'error')
  return data as T
}

export const get = <T = any>(p: string) => api<T>('GET', p)
export const post = <T = any>(p: string, b?: unknown) => api<T>('POST', p, b ?? {})
export const patch = <T = any>(p: string, b: unknown) => api<T>('PATCH', p, b)
export const put = <T = any>(p: string, b: unknown) => api<T>('PUT', p, b)
export const del = <T = any>(p: string) => api<T>('DELETE', p)

export const errText: Record<string, string> = {
  conflict: 'Партнёр как раз ответил — обновил экран.',
  closed: 'Этот выбор уже закрыт.',
  wrong_state: 'Этот шаг уже пройден.',
  not_paired: 'Сначала пригласи партнёра.',
  bad_time: 'Это время уже прошло или написано не так.',
  round_open: 'У вас уже идёт выбор — сначала завершите его.',
  no_ideas: 'В копилке нечего предложить.',
  limit: 'На сегодня вопросов достаточно — продолжим завтра.',
  voice_failed: 'Не получилось разобрать голос — напиши текстом.',
  audio_too_long: 'Слишком длинно — скажи покороче.',
  unauthorized: 'Открой приложение из Telegram.',
}
export const humanError = (e: unknown) => (e instanceof ApiError && errText[e.code]) || 'Не получилось. Попробуй ещё раз.'
