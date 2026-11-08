// Custom icon set «Вдвоём»: 24px grid, 1.8 stroke, round caps. Shapes echo the mark — circles and lenses.
import type { CSSProperties } from 'react'

const P: Record<string, JSX.Element> = {
  home: <><path d="M4 10.5 12 4l8 6.5V19a1 1 0 0 1-1 1h-4.5v-5.5h-5V20H5a1 1 0 0 1-1-1z" /></>,
  jar: <><path d="M7 4h10M8 4v2.2c0 .5-.3 1-.7 1.3A6 6 0 0 0 5 12.3V18a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-5.7a6 6 0 0 0-2.3-4.8c-.4-.3-.7-.8-.7-1.3V4" /><circle cx="10.5" cy="14" r="2.2" /><circle cx="13.5" cy="14" r="2.2" /></>,
  spark: <><path d="M12 3.5c.6 3.9 2.6 5.9 6.5 6.5-3.9.6-5.9 2.6-6.5 6.5-.6-3.9-2.6-5.9-6.5-6.5 3.9-.6 5.9-2.6 6.5-6.5Z" /><path d="M18.5 15.5c.3 1.6 1 2.3 2.5 2.5-1.5.3-2.2 1-2.5 2.5-.3-1.5-1-2.2-2.5-2.5 1.5-.2 2.2-.9 2.5-2.5Z" /></>,
  us: <><circle cx="9" cy="12" r="5.5" /><circle cx="15" cy="12" r="5.5" /></>,
  plus: <><path d="M12 5v14M5 12h14" /></>,
  cal: <><rect x="4" y="5.5" width="16" height="14.5" rx="3" /><path d="M8 3.5v4M16 3.5v4M4 10h16" /></>,
  clock: <><circle cx="12" cy="12" r="8" /><path d="M12 7.5V12l3 2" /></>,
  pin: <><path d="M12 21s-6.5-5.6-6.5-11a6.5 6.5 0 0 1 13 0c0 5.4-6.5 11-6.5 11Z" /><circle cx="12" cy="10" r="2.3" /></>,
  wallet: <><path d="M4.5 7.5h13a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-11a2 2 0 0 1-2-2v-10a2 2 0 0 1 2-2h10" /><circle cx="16" cy="13.5" r="1.2" fill="currentColor" stroke="none" /></>,
  dice: <><rect x="4" y="4" width="16" height="16" rx="4.5" /><circle cx="9" cy="9" r="1.3" fill="currentColor" stroke="none" /><circle cx="15" cy="15" r="1.3" fill="currentColor" stroke="none" /><circle cx="15" cy="9" r="1.3" fill="currentColor" stroke="none" /><circle cx="9" cy="15" r="1.3" fill="currentColor" stroke="none" /></>,
  back: <><path d="M14.5 5.5 8 12l6.5 6.5" /></>,
  chev: <><path d="m9.5 6 6 6-6 6" /></>,
  check: <><path d="m5 12.5 4.5 4.5L19 7.5" /></>,
  x: <><path d="M6.5 6.5 17.5 17.5M17.5 6.5 6.5 17.5" /></>,
  link: <><path d="M10 14a4 4 0 0 0 5.7 0l3-3a4 4 0 0 0-5.7-5.7l-1 1" /><path d="M14 10a4 4 0 0 0-5.7 0l-3 3a4 4 0 0 0 5.7 5.7l1-1" /></>,
  edit: <><path d="M4.5 19.5 5.3 16 15.8 5.5a2 2 0 0 1 2.7 2.7L8 18.7z" /><path d="M14 7.3 16.7 10" /></>,
  trash: <><path d="M5 7h14M10 4h4M7 7l.8 11.2A2 2 0 0 0 9.8 20h4.4a2 2 0 0 0 2-1.8L17 7" /></>,
  share: <><path d="M12 15V4M8 7.5 12 3.5l4 4" /><path d="M7 11H6a2 2 0 0 0-2 2v5a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-5a2 2 0 0 0-2-2h-1" /></>,
  bell: <><path d="M6 16.5V11a6 6 0 0 1 12 0v5.5l1.5 2h-15z" /><path d="M10 20.5a2 2 0 0 0 4 0" /></>,
  gear: <><circle cx="12" cy="12" r="3" /><path d="M12 3.5v2.2M12 18.3v2.2M20.5 12h-2.2M5.7 12H3.5M18 6l-1.6 1.6M7.6 16.4 6 18M18 18l-1.6-1.6M7.6 7.6 6 6" /></>,
  history: <><path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3L4.5 9" /><path d="M4.5 4.5V9H9M12 8v4.2l2.8 1.8" /></>,
  send: <><path d="M12 19V5.5M6 11l6-6 6 6" /></>,
  mic: <><rect x="9" y="3.5" width="6" height="11" rx="3" /><path d="M5.5 11.5a6.5 6.5 0 0 0 13 0M12 18v2.5" /></>,
  stop: <><rect x="7" y="7" width="10" height="10" rx="2.5" /></>,
  shuffle: <><path d="M4 7h3.5c4 0 5 10 9 10H20M4 17h3.5c1.4 0 2.4-1.2 3.3-2.8M13.2 9.8C14.1 8.2 15.1 7 16.5 7H20" /><path d="m17.5 4.5 2.5 2.5-2.5 2.5M17.5 14.5l2.5 2.5-2.5 2.5" /></>,
  heart: <><path d="M12 19.5s-7.5-4.4-7.5-10A4.3 4.3 0 0 1 12 6.8a4.3 4.3 0 0 1 7.5 2.7c0 5.6-7.5 10-7.5 10Z" /></>,
  moon: <><path d="M19 14.5A7.5 7.5 0 0 1 9.5 5a7.5 7.5 0 1 0 9.5 9.5Z" /></>,
  sun: <><circle cx="12" cy="12" r="4" /><path d="M12 2.5v2M12 19.5v2M21.5 12h-2M4.5 12h-2M18.7 5.3l-1.4 1.4M6.7 17.3l-1.4 1.4M18.7 18.7l-1.4-1.4M6.7 6.7 5.3 5.3" /></>,
  sunrise: <><path d="M5 17a7 7 0 0 1 14 0M3 20.5h18M12 3.5v4M8.5 6.5 12 3.5l3.5 3M4.6 10.6 6 12M19.4 10.6 18 12" /></>,
  chore: <><path d="M9 4.5h6M9.5 4.5 8.5 9h7l-1-4.5" /><path d="M8.5 9 6 20h12L15.5 9" /><path d="M10 13.5h4" /></>,
  photo: <><rect x="3.5" y="5.5" width="17" height="14" rx="3.5" /><circle cx="12" cy="12.5" r="3.5" /><path d="M8.5 5.5 10 3.5h4l1.5 2" /></>,
  food: <><path d="M6.5 3.5v6.5a2.5 2.5 0 0 0 5 0V3.5M9 3.5v17M16.5 20.5V3.5c-2 0-3.5 2.5-3.5 6.5s1.5 4.5 3.5 4.5" /></>,
  culture: <><path d="M5.5 5.5h13v5.5a6.5 6.5 0 0 1-13 0z" /><path d="M9 9.5h.01M15 9.5h.01M9.5 13.5a3.5 3.5 0 0 0 5 0" /></>,
  walk: <><path d="M12 21v-6M7 15h10l-5-11z" /><path d="M8.5 11h7" /></>,
  sport: <><circle cx="12" cy="12" r="8" /><path d="M4.5 9.5c4 1 10.5 1 15 0M4.5 14.5c4-1 10.5-1 15 0M12 4c-2.5 4-2.5 12 0 16" /></>,
  face: <><path d="M4 8V6.5A2.5 2.5 0 0 1 6.5 4H8M16 4h1.5A2.5 2.5 0 0 1 20 6.5V8M20 16v1.5a2.5 2.5 0 0 1-2.5 2.5H16M8 20H6.5A2.5 2.5 0 0 1 4 17.5V16" /><path d="M9 9.5v1M15 9.5v1M12 9.5v3.5h-1M9.5 15.5c1.4 1.2 3.6 1.2 5 0" /></>,
  lock: <><rect x="5" y="10.5" width="14" height="9.5" rx="3" /><path d="M8.5 10.5V8a3.5 3.5 0 0 1 7 0v2.5M12 14.5v2" /></>,
  user: <><circle cx="12" cy="8.5" r="3.8" /><path d="M4.5 20c1.2-3.6 4-5.3 7.5-5.3s6.3 1.7 7.5 5.3" /></>,
  plane: <><path d="M21 4 3 11.2l6.6 2.4L12 20.5l3-5.3L21 4Z" /><path d="m9.6 13.6 5.4-4" /></>,
  folder: <><path d="M3.5 7.5A2.5 2.5 0 0 1 6 5h3.6l2 2.2H18a2.5 2.5 0 0 1 2.5 2.5v7.8A2.5 2.5 0 0 1 18 20H6a2.5 2.5 0 0 1-2.5-2.5z" /></>,
  chart: <><path d="M4 19.5h16" /><path d="M5 15c2.5 0 3-5 5.5-5s2.5 3 5 3 2.5-6 4-7" /></>,
  search: <><circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" /></>,
  flame: <><path d="M12 21c3.6 0 6-2.4 6-5.8 0-4-3.6-6.2-4.5-10.2-2.2 1.6-3.3 3.6-3.4 6-1-.7-1.6-1.8-1.8-3C6.8 9.6 6 11.8 6 15.2 6 18.6 8.4 21 12 21Z" /></>,
  other: <><circle cx="12" cy="12" r="8" /><path d="M12 8v4l2.5 2.5" /></>,
}

export function Icon({ n, s = 22, w = 1.8, style, className }: { n: keyof typeof P | string; s?: number; w?: number; style?: CSSProperties; className?: string }) {
  return (
    <svg width={s} height={s} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={w} strokeLinecap="round" strokeLinejoin="round"
      style={style} className={className} aria-hidden="true">
      {P[n] || P.other}
    </svg>
  )
}
