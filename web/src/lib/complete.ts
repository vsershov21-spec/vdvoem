// Completion while typing — local and instant: phrases the assistant understands for sure, plus the titles
// in the bank and the afisha. No model, no network after the first load.

const norm = (s: string) => s.toLowerCase().replace(/ё/g, 'е').replace(/[«»"?!.,]/g, ' ').replace(/\s+/g, ' ').trim()
const words = (s: string) => norm(s).split(' ').filter(Boolean)

// «сколько стоит чай» → the verb part that a title completes
const LEADS: [RegExp, (t: string) => string][] = [
  [/^(сколько стоит|почем|цена)\s+(.*)$/, t => `Сколько стоит «${t}»?`],
  [/^(расскажи про|расскажи о|что за|что такое)\s+(.*)$/, t => `Расскажи про «${t}»`],
  [/^(запиши нас на|запиши на|давай на|идем на|пойдем на)\s+(.*)$/, t => `Запиши нас на «${t}»`],
  [/^(следи за|следи за билетами на)\s+(.*)$/, t => `Следи за билетами на «${t}»`],
]

function prefixMatch(input: string[], target: string[]) {
  // every typed word starts some word of the target, in order; the last one may be unfinished
  let k = 0
  for (const w of input) {
    while (k < target.length && !target[k].startsWith(w)) k++
    if (k === target.length) return false
    k++
  }
  return true
}

export function complete(text: string, phrases: string[], titles: string[], n = 3): string[] {
  const q = norm(text)
  if (q.length < 2) return []
  const out: string[] = []
  const add = (s: string) => { if (!out.includes(s) && norm(s) !== q && out.length < n) out.push(s) }
  for (const [re, make] of LEADS) {
    const m = q.match(re)
    if (!m) continue
    const rest = words(m[2])
    if (rest.length && rest.join('').length >= 2) for (const t of titles) if (prefixMatch(rest, words(t))) add(make(t))
    if (out.length) return out
  }
  const qw = words(text)
  for (const p of phrases) if (prefixMatch(qw, words(p))) add(p)
  if (q.length >= 3) for (const t of titles) if (prefixMatch(qw, words(t))) add(`Расскажи про «${t}»`)
  return out
}
