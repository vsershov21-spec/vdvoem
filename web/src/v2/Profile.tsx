import { useState } from 'react'
import { useLoad } from '../lib/data'
import { post, put, humanError } from '../lib/api'
import { go, back } from '../lib/router'
import { emit } from '../lib/data'
import { haptic } from '../lib/tg'
import { when as whenText, ncase } from '../lib/format'
import type { Round } from '../lib/types'
import type { HomeV2 } from './types'
import { Pic, Empty, Sheet, say } from './ui'
import type { Chore } from './Chores'

const Sub = ({ from, title }: { from: string; title: string }) => <div className="subh"><button className="back" aria-label="Назад" onClick={() => back('')}>‹ {from}</button><b>{title}</b><span /></div>
const A = (n?: string) => (n || '?')[0]
const rate = (who: string, a?: string) => `${who}: ${a === 'yes' ? 'понравилось' : a === 'no' ? 'не очень' : a === 'skip' ? 'не ходили' : 'без оценки'}`

// ---------- profile: the pair, the history of meetings and chores, settings ----------
export function Profile({ home, from }: { home: HomeV2 | null; from: string }) {
  const meets = useLoad<{ meetings: Round[] }>('/history')
  const chores = useLoad<{ chores: Chore[] }>('/chores/history')
  if (!home) return <div className="scr"><Sub from={from} title="Профиль" /></div>
  return (
    <div className="scr" data-testid="v2-us">
      <Sub from={from} title="Профиль" />
      <div className="v-card"><div className="v-row"><span className="avs"><i>{A(home.me.name)}</i>{home.partner && <i>{A(home.partner.name)}</i>}</span>
        <span><b style={{ fontSize: 17 }}>{home.partner ? `${home.me.name} и ${home.partner.name}` : home.me.name}</b><p className="m">{home.partner ? 'в паре' : 'партнёра пока нет'}</p></span></div></div>
      <div className="tiles three">
        <div className="in"><span><b>{meets.data?.meetings.length ?? '·'}</b><small>встречи</small></span></div>
        <div className="in"><span><b>{chores.data?.chores.length ?? '·'}</b><small>дел сделано</small></span></div>
        <div className="in"><span><b>{home.counts.both_want}</b><small>совпало</small></span></div>
      </div>
      <div className="blk"><label>История</label><div className="v-card v-list">
        <button className="srow" data-testid="hist-meets" onClick={() => go('history/meets')}><span className="tx"><b>Встречи</b><small>где были и как понравилось</small></span>›</button>
        <button className="srow" data-testid="hist-chores" onClick={() => go('history/chores')}><span className="tx"><b>Дела</b><small>кто что сделал и когда</small></span>›</button></div></div>
      <div className="blk"><label>Я</label><div className="v-card v-list">
        <button className="srow" data-testid="to-taste" onClick={() => go('taste')}><span className="tx"><b>Мой вкус</b><small>что нравится: по этому подбираются идеи</small></span>›</button>
        <button className="srow" data-testid="to-settings" onClick={() => go('settings')}><span className="tx"><b>Настройки</b><small>уведомления, календарь, пара</small></span>›</button></div></div>
    </div>)
}

export function History({ view, home, from }: { view: string; home: HomeV2 | null; from: string }) {
  const tab = view === 'chores' ? 'chores' : 'meets'
  const [q, setQ] = useState('')
  const [who, setWho] = useState<'' | 'me' | 'partner'>('')
  const meets = useLoad<{ meetings: Round[] }>(tab === 'meets' ? '/history' : null)
  const chores = useLoad<{ chores: Chore[] }>(tab === 'chores' ? `/chores/history?q=${encodeURIComponent(q)}&who=${who}` : null)
  const pn = home?.partner?.name || 'партнёр'
  return (
    <div className="scr" data-testid="v2-history">
      <Sub from={from} title="История" />
      <div className="v-seg" role="tablist"><button role="tab" aria-selected={tab === 'meets'} className={tab === 'meets' ? 'on' : ''} onClick={() => go('history/meets', true)}>Встречи</button><button role="tab" aria-selected={tab === 'chores'} className={tab === 'chores' ? 'on' : ''} onClick={() => go('history/chores', true)}>Дела</button></div>
      {tab === 'meets' ? (meets.data && (meets.data.meetings.length ? meets.data.meetings.map(r =>
        <button className="in" key={r.id} data-testid="hist-meet" onClick={() => go('meet/' + r.id)}>{r.chosen && <Pic idea={r.chosen} className="th" />}<span><b>{r.chosen?.title || 'Встреча'}</b>
          <small>{r.starts_at ? whenText(r.starts_at) : ''}</small><small data-testid="hist-rates">{rate('ты', r.again)} · {rate(pn, r.partner_again)}</small></span>›</button>)
        : <Empty title="Встреч ещё не было"><button className="b p w" onClick={() => go('ideas')}>Выбрать, куда сходить</button></Empty>))
        : <>
          <label className="in"><i>⌕</i><span><input value={q} onChange={e => setQ(e.target.value)} placeholder="Найти: «кран», «подарок»" aria-label="Поиск по делам" data-testid="hist-search" /></span></label>
          <div className="v-chips">{([['', 'Все'], ['me', home?.me.name || 'Я'], ['partner', pn]] as ['' | 'me' | 'partner', string][]).map(([k, l]) => <button key={k} className={'v-chip' + (who === k ? ' on' : '')} onClick={() => setWho(k)}>{l}</button>)}</div>
          {chores.data && (chores.data.chores.length ? chores.data.chores.map(c =>
            <div className="in" key={c.id} data-testid="hist-chore"><span className="ck on" style={{ display: 'grid', placeItems: 'center' }}>✓</span><span><b>{c.title}</b><small>{c.done_by} · {c.done_at ? new Date(c.done_at * 1000).toLocaleDateString('ru', { weekday: 'short', day: 'numeric', month: 'short' }) : ''}</small></span></div>)
            : <Empty title={q || who ? 'Ничего не нашлось' : 'Сделанных дел пока нет'}><button className="b p w" onClick={() => go('chores')}>К делам</button></Empty>)}
        </>}
    </div>)
}

// ---------- settings: mine, the couple's; the row shows the current value, inside are switches and choices ----------
type Def = { key: string; scope: 'me' | 'pair'; kind: 'bool' | 'choice' | 'text'; default: string; values?: string[]; label: string }
type Prefs = { me: Record<string, string>; pair: Record<string, string>; defs: Def[] }
const WORDS: Record<string, string> = { '2h': 'за 2 часа', day: 'за день', both: 'оба раза', manual: 'отмечаю сам', calendar: 'из календаря', '3000': 'до 3 000 ₽', '5000': 'до 5 000 ₽', none: 'без лимита',
  '15': 'пешком до 15 мин', '30': 'пешком до 30 мин', transit: 'на транспорте', daily: 'каждый день', weekdays: 'по будням', weekly: 'раз в неделю', '9': 'в 9:00', '19': 'в 19:00', off: 'не напоминать', '7': '7 дней' }
const word = (key: string, v: string) => key === 'chores.keep' ? v + ' дней' : WORDS[v] || v
const SECTIONS: { id: string; title: string; scope: 'me' | 'pair'; keys: string[]; icon: string; note?: string }[] = [
  { id: 'notify', title: 'Уведомления', scope: 'me', keys: ['notify.proposals', 'notify.meeting', 'notify.meeting_when', 'notify.chores', 'notify.scout', 'notify.after', 'notify.quiet'], icon: '<path d="M6 16V11a6 6 0 0 1 12 0v5l1.500 2h-15z"/><path d="M10 20a2 2 0 0 0 4 0"/>', note: 'На главной видно всё. Здесь: что дублировать в бот.' },
  { id: 'cal', title: 'Календарь', scope: 'me', keys: ['cal.meetings', 'cal.busy'], icon: '<rect x="4.500" y="5.500" width="15" height="14" rx="3"/><path d="M4.500 10h15M9 3.500v4M15 3.500v4"/>' },
  { id: 'meet', title: 'Встречи', scope: 'pair', keys: ['meet.budget', 'meet.walk'], icon: '<path d="M12 21s-6.500-5.600-6.500-10.500a6.500 6.500 0 0 1 13 0C18.500 15.400 12 21 12 21z"/><circle cx="12" cy="10.500" r="2.300"/>' },
  { id: 'scout', title: 'Скаут и афиша', scope: 'pair', keys: ['scout.kino', 'scout.concert', 'scout.theatre', 'scout.food', 'scout.active', 'scout.freq'], icon: '<circle cx="11" cy="11" r="6"/><path d="M15.500 15.500L20 20"/>' },
  { id: 'chores', title: 'Дела', scope: 'pair', keys: ['chores.remind', 'chores.keep', 'chores.overdue_home'], icon: '<rect x="4.500" y="4.500" width="15" height="15" rx="4"/><path d="M8.500 12.200l2.400 2.400 4.600-5"/>' },
]
const summary = (s: typeof SECTIONS[number], p: Prefs) => {
  const v = p[s.scope], bools = s.keys.filter(k => p.defs.find(d => d.key === k)?.kind === 'bool'), on = bools.filter(k => v[k] === '1').length
  const rest = s.keys.filter(k => !bools.includes(k)).map(k => word(k, v[k]))
  return [bools.length ? `включено ${on} из ${bools.length}` : '', ...rest].filter(Boolean).join(' · ')
}

export function Settings({ section, home, from }: { section?: string; home: HomeV2 | null; from: string }) {
  const { data, set } = useLoad<Prefs>('/prefs')
  const s = SECTIONS.find(x => x.id === section)
  const change = async (key: string, value: string) => {
    try { set(await put<Prefs>('/prefs', { key, value })); if (data?.defs.find(d => d.key === key)?.scope === 'pair' && home?.partner) say(`${home.partner.name} увидит изменение`) } catch (e) { say(humanError(e)) }
  }
  if (section === 'pair') return <Pair home={home} from={from} />
  if (section === 'data') return <Data home={home} from={from} />
  if (!data) return <div className="scr"><Sub from={from} title={s?.title || 'Настройки'} /></div>
  if (s) {
    const v = data[s.scope], defs = s.keys.map(k => data.defs.find(d => d.key === k)!).filter(Boolean)
    const bools = defs.filter(d => d.kind === 'bool')
    return (
      <div className="scr" data-testid="v2-set">
        <Sub from={from} title={s.title} />
        {s.note && <p className="m">{s.note}</p>}
        {s.scope === 'pair' && <p className="m">Общая настройка: меняет любой из вас, второй получает строку на главной.</p>}
        {bools.length > 0 && <div className="v-card v-list">{bools.map(d => <div className="srow" key={d.key}><span className="tx"><b>{d.label}</b></span>
          <button className={'tg' + (v[d.key] === '1' ? ' on' : '')} role="switch" aria-checked={v[d.key] === '1'} aria-label={d.label} data-testid={'set-' + d.key} onClick={() => change(d.key, v[d.key] === '1' ? '0' : '1')} /></div>)}</div>}
        {defs.filter(d => d.kind === 'choice').map(d => <div className="blk" key={d.key}><label>{d.label}</label><div className="v-row" style={{ flexWrap: 'wrap' }}>
          {d.values!.map(x => <button key={x} className={'v-chip' + (v[d.key] === x ? ' on' : '')} aria-pressed={v[d.key] === x} data-testid={`set-${d.key}-${x}`} onClick={() => change(d.key, x)}>{word(d.key, x)}</button>)}</div></div>)}
        {s.id === 'notify' && <p className="m" data-testid="quiet-note">Тихие часы: с 23:00 до 9:00 сообщения бота приходят без звука.</p>}
        {s.id === 'cal' && <p className="m">{v['cal.busy'] === 'manual' ? 'Занятые дни отмечаются в Планах: тап по дню, «Я занят».' : 'Ссылка на календарь подключается в Планах, кнопка «Календарь».'}</p>}
      </div>)
  }
  const row = (x: typeof SECTIONS[number]) => <button className="srow" key={x.id} data-testid={'sec-' + x.id} onClick={() => go('settings/' + x.id)}><i className="si"><svg viewBox="0 0 24 24" dangerouslySetInnerHTML={{ __html: x.icon }} /></i><span className="tx"><b>{x.title}</b><small>{summary(x, data)}</small></span>›</button>
  return (
    <div className="scr" data-testid="v2-settings">
      <Sub from={from} title="Настройки" />
      <div className="blk"><label>Для меня</label><div className="v-card v-list">{SECTIONS.filter(x => x.scope === 'me').map(row)}</div></div>
      <div className="blk"><label>Для пары</label><div className="v-card v-list">{SECTIONS.filter(x => x.scope === 'pair').map(row)}
        <button className="srow" data-testid="sec-pair" onClick={() => go('settings/pair')}><i className="si"><svg viewBox="0 0 24 24"><circle cx="9" cy="12" r="5" /><circle cx="15" cy="12" r="5" /></svg></i><span className="tx"><b>Пара</b><small>{home?.partner ? home.partner.name : 'партнёра пока нет'}</small></span>›</button></div></div>
      <div className="blk"><label>Вход</label><div className="v-card v-list"><button className="srow" onClick={async () => { try { await post('/security/logout-all'); say('Остальные устройства отключены') } catch (e) { say(humanError(e)) } }}><span className="tx"><b>Выйти на других устройствах</b><small>вход через Telegram останется на этом</small></span>›</button></div></div>
      <div className="blk"><label>Данные</label><div className="v-card v-list">
        <button className="srow" data-testid="sec-data" onClick={() => go('settings/data')}><i className="si"><svg viewBox="0 0 24 24"><path d="M12 4v11M7.500 10.500L12 15l4.500-4.500M5 19h14" /></svg></i><span className="tx"><b>Данные</b><small>скачать, удалить</small></span>›</button></div></div>
    </div>)
}

// ---------- «Пара»: who the partner is, since when, and the way out — only through the sheet with what it means ----------
function Pair({ home, from }: { home: HomeV2 | null; from: string }) {
  const [ask, setAsk] = useState(false)
  const [busy, setBusy] = useState(false)
  const p = home?.partner
  const leave = async () => {
    setBusy(true)
    try {
      const r = await post<{ ideas: number }>('/pair/leave')
      haptic.ok(); setAsk(false); emit('home'); emit('ideas'); go('', true)
      say(r.ideas > 0 ? 'Ты больше не в паре. Копия твоих идей с тобой' : 'Ты больше не в паре')
    } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  return (
    <div className="scr" data-testid="v2-pair">
      <Sub from={from} title="Пара" />
      {p ? <>
        <div className="v-card v-list"><div className="srow"><span className="tx"><b>{p.name}</b>
          <small>{home?.paired_since ? 'в паре с ' + new Date(home.paired_since * 1000).toLocaleDateString('ru', { day: 'numeric', month: 'long' }) : 'в паре'}</small></span></div></div>
        <button className="b w red" data-testid="leave" onClick={() => setAsk(true)}>Выйти из пары</button>
      </> : home && <Empty title="Партнёра пока нет"><button className="b p w" onClick={() => go('', true)}>Пригласить с главной</button></Empty>}
      <Sheet open={ask && !!p} onClose={() => setAsk(false)} testid="leave-sheet">
        <h3>Выйти из пары?</h3>
        <ul className="ul m">
          <li>Общие идеи, встречи и история останутся у {ncase(p?.name || '', 'gen')}.</li>
          <li>Ты получишь копию своих идей.</li>
          <li>Назначенные встречи отменятся, {p?.name} узнает об этом.</li>
        </ul>
        <button className="b w red" disabled={busy} data-testid="leave-confirm" onClick={leave}>Выйти из пары</button>
        <button className="b w" onClick={() => setAsk(false)}>Остаться</button>
      </Sheet>
    </div>)
}

// ---------- «Данные»: my ideas and history as a file in the bot; deleting myself, confirmed by a typed word ----------
const WORD = 'удалить'
function Data({ home, from }: { home: HomeV2 | null; from: string }) {
  const [ask, setAsk] = useState(false)
  const [word, setWord] = useState('')
  const [busy, setBusy] = useState(false)
  const p = home?.partner
  const file = async () => {
    setBusy(true)
    try { await post('/data/export'); say('Файл придёт в бот') } catch (e) { say(humanError(e)) } finally { setBusy(false) }
  }
  const wipe = async () => {
    setBusy(true)
    try {
      await post('/data/delete', { confirm: word })
      go('', true); window.location.reload() // nothing is left of this person: the app starts as for a new one
    } catch (e) { say(humanError(e)); setBusy(false) }
  }
  return (
    <div className="scr" data-testid="v2-data">
      <Sub from={from} title="Данные" />
      <div className="v-card v-list">
        <button className="srow" disabled={busy} data-testid="data-export" onClick={file}><span className="tx"><b>Скачать мои идеи и историю</b><small>файл придёт в бот</small></span>›</button>
        <button className="srow red" data-testid="data-delete" onClick={() => { setWord(''); setAsk(true) }}><span className="tx"><b>Удалить мои данные</b><small>вернуть нельзя</small></span>›</button>
      </div>
      <Sheet open={ask} onClose={() => setAsk(false)} testid="wipe-sheet">
        <h3>Удалить мои данные?</h3>
        <ul className="ul m">
          {p && <li>Ты выйдешь из пары. Общие идеи, встречи и дела останутся у {ncase(p.name, 'gen')} без твоего имени, отметок и оценок.</li>}
          <li>Удалятся твои идеи и отметки, вкус, память и переписка с помощником.</li>
          <li>Удалятся настройки, занятые дни и входы на всех устройствах.</li>
          <li>Вернуть нельзя.</li>
        </ul>
        <label className="fld"><small>Чтобы подтвердить, напиши слово «{WORD}»</small>
          <input value={word} onChange={e => setWord(e.target.value)} autoCapitalize="off" autoComplete="off" placeholder={WORD} aria-label="Слово для подтверждения" data-testid="wipe-word" /></label>
        <button className="b w red" disabled={busy || word.trim().toLowerCase() !== WORD} data-testid="wipe-confirm" onClick={wipe}>Удалить навсегда</button>
        <button className="b w" onClick={() => setAsk(false)}>Отмена</button>
      </Sheet>
    </div>)
}

// ---------- «Мой вкус»: one tap — нравится; the scout and the assistant pick ideas by it ----------
type Sphere = { id: string; title: string; emoji: string; tags: string[] }
type MyTaste = { spheres: Record<string, number>; tags: Record<string, number>; [k: string]: unknown }
export function Taste({ from }: { from: string }) {
  const { data, set } = useLoad<{ spheres: Sphere[]; me: MyTaste }>('/taste')
  const save = async (me: MyTaste) => {
    set(d => d && { ...d, me })
    try { const r = await put<{ me: MyTaste }>('/taste', me); set(d => d && { ...d, me: r.me }) } catch (e) { say(humanError(e)) }
  }
  if (!data) return <div className="scr"><Sub from={from} title="Мой вкус" /></div>
  const me = { ...data.me, spheres: data.me.spheres || {}, tags: data.me.tags || {} }
  return (
    <div className="scr" data-testid="v2-taste">
      <Sub from={from} title="Мой вкус" />
      <p className="m">Отметь, что нравится. По этому скаут и помощник подбирают идеи, а отметки на карточках уточняют остальное.</p>
      {data.spheres.map(s => { const on = (me.spheres[s.id] || 0) > 0
        return <div className="blk" key={s.id}>
          <button className={'v-chip' + (on ? ' on' : '')} style={{ alignSelf: 'flex-start' }} aria-pressed={on} data-testid={'sphere-' + s.id} onClick={() => save({ ...me, spheres: { ...me.spheres, [s.id]: on ? 0 : 1 } })}>{s.title}</button>
          {on && <div className="v-row" style={{ flexWrap: 'wrap' }}>{s.tags.map(t => { const tOn = (me.tags[t] || 0) > 0
            return <button key={t} className={'v-chip' + (tOn ? ' on' : '')} aria-pressed={tOn} onClick={() => save({ ...me, tags: { ...me.tags, [t]: tOn ? 0 : 1 } })}>{t}</button> })}</div>}
        </div> })}
    </div>)
}
