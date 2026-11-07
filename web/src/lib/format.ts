const NB = ' ', NNB = ' '
export const money = (kop?: number) => {
  if (!kop) return ''
  const r = Math.round(kop / 100)
  return r.toString().replace(/\B(?=(\d{3})+(?!\d))/g, NNB) + NB + '₽'
}
const WD = ['Вс', 'Пн', 'Вт', 'Ср', 'Чт', 'Пт', 'Сб']
const WDL = ['воскресенье', 'понедельник', 'вторник', 'среда', 'четверг', 'пятница', 'суббота']
const MON = ['янв', 'фев', 'мар', 'апр', 'мая', 'июн', 'июл', 'авг', 'сен', 'окт', 'ноя', 'дек']
const pad = (n: number) => String(n).padStart(2, '0')
export const hm = (t: number) => { const d = new Date(t * 1000); return `${pad(d.getHours())}:${pad(d.getMinutes())}` }
export const dayShort = (t: number) => { const d = new Date(t * 1000); return `${WD[d.getDay()]}, ${d.getDate()} ${MON[d.getMonth()]}` }
export const when = (from: number, to?: number) => `${dayShort(from)} · ${hm(from)}${to ? '–' + hm(to) : ''}`
export const parseDay = (s: string) => { const [y, m, d] = s.split('-').map(Number); return new Date(y, m - 1, d) }
export const dayLabel = (s: string) => { const d = parseDay(s); return { wd: WD[d.getDay()], long: WDL[d.getDay()], num: d.getDate(), mon: MON[d.getMonth()] } }
export const dur = (m?: number) => !m ? '' : m % 60 === 0 ? `${m / 60}${NB}ч` : m > 60 ? `${(m / 60).toFixed(1).replace('.', ',')}${NB}ч` : `${m}${NB}мин`
export const plural = (n: number, one: string, few: string, many: string) => {
  const a = Math.abs(n) % 100, b = a % 10
  return a > 10 && a < 20 ? many : b > 1 && b < 5 ? few : b === 1 ? one : many
}
export const relLeft = (t: number) => {
  const h = Math.round((t * 1000 - Date.now()) / 36e5)
  if (h <= 0) return 'скоро закроется'
  if (h < 24) return `ещё ${h} ${plural(h, 'час', 'часа', 'часов')}`
  const d = Math.round(h / 24); return `ещё ${d} ${plural(d, 'день', 'дня', 'дней')}`
}
export const CATS: Record<string, { name: string; tone: string; emoji: string }> = {
  food: { name: 'Еда', tone: 'ember', emoji: '🍽' },
  culture: { name: 'Культура', tone: 'onyx', emoji: '🎭' },
  walk: { name: 'Прогулка', tone: 'forest', emoji: '🌿' },
  sport: { name: 'Спорт', tone: 'ocean', emoji: '⛸' },
  home: { name: 'Дела', tone: 'onyx', emoji: '🏠' },
  other: { name: 'Другое', tone: 'ocean', emoji: '✨' },
}

// Russian name cases for partner names in UI text: «ждём Настю», «с Настей», «голос Насти».
// Simple endings only; anything unusual (Latin, -о, -и, compound) stays as is.
export function ncase(name: string, c: 'gen' | 'acc' | 'ins'): string {
  const n = name.trim()
  if (!/^[А-ЯЁ][а-яё]+$/.test(n)) return n
  const stem = n.slice(0, -1), last = n.slice(-1), pre = n.slice(-2, -1)
  const hush = /[жшщчц]/.test(pre), soft = /[гкхжшщч]/.test(pre)
  if (last === 'я') return stem + (c === 'gen' ? 'и' : c === 'acc' ? 'ю' : 'ей')
  if (last === 'а') return stem + (c === 'gen' ? (soft ? 'и' : 'ы') : c === 'acc' ? 'у' : (hush ? 'ей' : 'ой'))
  if (last === 'й') return stem + (c === 'ins' ? 'ем' : 'я')
  if (last === 'ь') return stem + (c === 'ins' ? 'ем' : 'я')
  if (/[бвгджзклмнпрстфхцчшщ]/.test(last)) return n + (c === 'ins' ? (/[жшщчц]/.test(last) ? 'ем' : 'ом') : 'а')
  return n
}
