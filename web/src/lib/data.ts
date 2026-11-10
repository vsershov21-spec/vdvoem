import { useCallback, useEffect, useRef, useState } from 'react'
import { get } from './api'
import { live } from './live'

// useLoad fetches on mount, on refresh(), every `poll` ms while the page is visible
// (so the partner's moves appear without reload), and when the app comes back to the foreground.
// Stale-while-revalidate: the last response per path is kept, so a screen opens instantly with what
// it showed before and quietly refreshes — no skeleton flash on every tab switch.
const cache = new Map<string, unknown>()
export const cached = (path: string) => cache.has(path)
export function prefetch(paths: string[]) {
  for (const p of paths) if (!cache.has(p)) get(p).then(d => cache.set(p, d)).catch(() => {})
}

// patch: change what a path shows from outside a screen — an answer that arrives after the screen was left
// still lands in its cache and shows when the screen opens again
export function patch<T>(path: string, f: (x: T | null) => T | null) {
  const v = f((cache.get(path) as T) ?? null)
  if (v) cache.set(path, v)
  emit('patch:' + path)
}

export function useLoad<T>(path: string | null, poll = 0) {
  const [data, setDataRaw] = useState<T | null>(() => (path && (cache.get(path) as T)) || null)
  const setData = useCallback((d: T | null | ((x: T | null) => T | null)) => {
    setDataRaw(prev => { const v = typeof d === 'function' ? (d as (x: T | null) => T | null)(prev) : d; if (path && v) cache.set(path, v); return v })
  }, [path])
  const [err, setErr] = useState<unknown>(null)
  const seq = useRef(0)
  const load = useCallback(async () => {
    if (!path) return
    const my = ++seq.current
    try { const d = await get<T>(path); if (my === seq.current) { setData(d); setErr(null) } }
    catch (e) { if (my === seq.current) setErr(e) }
  }, [path])
  useEffect(() => { setDataRaw((path && (cache.get(path) as T)) || null); load() }, [load])
  useEffect(() => {
    const vis = () => { if (document.visibilityState === 'visible') load() }
    document.addEventListener('visibilitychange', vis)
    // with the live stream up, polling is only a slow safety net
    let n = 0
    const t = poll ? setInterval(() => { if (document.visibilityState === 'visible' && (!live.up || ++n % 6 === 0)) load() }, poll) : 0
    return () => { document.removeEventListener('visibilitychange', vis); if (t) clearInterval(t) }
  }, [load, poll])
  useOn('sync', load) // the partner changed something: refetch now
  useOn('patch:' + (path || ''), useCallback(() => { if (path) setDataRaw((cache.get(path) as T) ?? null) }, [path]))
  return { data, err, refresh: load, set: setData }
}

// tiny event bus: screens tell each other «ideas changed», «round changed»
const bus = new Map<string, Set<() => void>>()
export const emit = (k: string) => bus.get(k)?.forEach(f => f())
export function useOn(k: string, f: () => void) {
  useEffect(() => { if (!bus.has(k)) bus.set(k, new Set()); bus.get(k)!.add(f); return () => { bus.get(k)!.delete(f) } }, [k, f])
}
