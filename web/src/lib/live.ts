// Real time: one server-sent-events stream per open app. «sync» → open screens refetch,
// «presence» → who of the two is in the app. Polling stays as a fallback while the stream is down.
import { useCallback, useState } from 'react'
import { authHeaders, CLIENT_ID } from './api'
import { emit, useOn } from './data'

export const live = { up: false, online: new Set<string>() }
let started = false, retry = 1000, ctl: AbortController | null = null

export function startLive() {
  if (started) return
  started = true
  connect()
  document.addEventListener('visibilitychange', () => { if (document.visibilityState === 'visible' && !live.up) { retry = 1000; connect() } })
  addEventListener('online', () => { if (!live.up) { retry = 1000; connect() } })
}

async function connect() {
  ctl?.abort()
  const my = (ctl = new AbortController())
  try {
    const res = await fetch('/api/live', { headers: authHeaders(), signal: my.signal, cache: 'no-store' })
    if (!res.ok || !res.body) throw new Error('live ' + res.status)
    const rd = res.body.getReader(), dec = new TextDecoder()
    let buf = ''
    for (;;) {
      const { value, done } = await rd.read()
      if (done) break
      buf += dec.decode(value, { stream: true })
      let i
      while ((i = buf.indexOf('\n\n')) >= 0) { handle(buf.slice(0, i)); buf = buf.slice(i + 2) }
    }
  } catch { /* aborted or offline */ }
  if (my !== ctl) return
  down()
  if (document.visibilityState === 'visible') { setTimeout(() => { if (!live.up && my === ctl) connect() }, retry); retry = Math.min(retry * 2, 15000) }
}

function handle(block: string) {
  let ev = 'message', data = ''
  for (const l of block.split('\n')) { if (l.startsWith('event: ')) ev = l.slice(7); else if (l.startsWith('data: ')) data += l.slice(6) }
  if (ev === 'hello') { const was = live.up; live.up = true; retry = 1000; if (!was) emit('sync') /* catch up on what we missed */; emit('live'); return }
  const d = data ? JSON.parse(data) : {}
  if (ev === 'sync' && d.cid !== CLIENT_ID) emit('sync')
  if (ev === 'presence') { live.online = new Set(d.online || []); emit('live') }
}

function down() { if (live.up) { live.up = false; live.online = new Set(); emit('live') } }

// is this person («a»/«b») in the app right now?
export function useOnline(color?: string) {
  const [, tick] = useState(0)
  useOn('live', useCallback(() => tick(n => n + 1), []))
  return !!color && live.online.has(color)
}
