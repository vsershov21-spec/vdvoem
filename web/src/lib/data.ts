import { useCallback, useEffect, useRef, useState } from 'react'
import { get } from './api'
import { live } from './live'

// useLoad fetches on mount, on refresh(), every `poll` ms while the page is visible
// (so the partner's moves appear without reload), and when the app comes back to the foreground.
export function useLoad<T>(path: string | null, poll = 0) {
  const [data, setData] = useState<T | null>(null)
  const [err, setErr] = useState<unknown>(null)
  const seq = useRef(0)
  const load = useCallback(async () => {
    if (!path) return
    const my = ++seq.current
    try { const d = await get<T>(path); if (my === seq.current) { setData(d); setErr(null) } }
    catch (e) { if (my === seq.current) setErr(e) }
  }, [path])
  useEffect(() => { setData(null); load() }, [load])
  useEffect(() => {
    const vis = () => { if (document.visibilityState === 'visible') load() }
    document.addEventListener('visibilitychange', vis)
    // with the live stream up, polling is only a slow safety net
    let n = 0
    const t = poll ? setInterval(() => { if (document.visibilityState === 'visible' && (!live.up || ++n % 6 === 0)) load() }, poll) : 0
    return () => { document.removeEventListener('visibilitychange', vis); if (t) clearInterval(t) }
  }, [load, poll])
  useOn('sync', load) // the partner changed something: refetch now
  return { data, err, refresh: load, set: setData }
}

// tiny event bus: screens tell each other «ideas changed», «round changed»
const bus = new Map<string, Set<() => void>>()
export const emit = (k: string) => bus.get(k)?.forEach(f => f())
export function useOn(k: string, f: () => void) {
  useEffect(() => { if (!bus.has(k)) bus.set(k, new Set()); bus.get(k)!.add(f); return () => { bus.get(k)!.delete(f) } }, [k, f])
}
