import { useState } from 'react'
import { useLoad, emit } from '../lib/data'
import { post, del } from '../lib/api'
import { openLink, haptic } from '../lib/tg'
import { Icon } from './icons'
import { Btn, Sheet, Skel, toast } from './kit'

// The phone's calendar, both ways, in one place:
//  • встречи → в телефон: a subscription to our meetings (the phone reads the feed by itself);
//  • занятость ← из телефона: the private iCal link of my calendar, so busy hours show when we pick the time.
// They are two different links, and people expect one «подключить» — so the sheet shows both with a status each.
export type Feed = { url: string; webcal: string; subscribed: boolean; busy?: boolean; partner_busy?: boolean }

const ADDED = 'vd-cal-added'
const wasAdded = () => { try { return !!localStorage.getItem(ADDED) } catch { return false } }
export const useFeed = () => useLoad<Feed>('/calendar/feed')

// connected — any of the two set up (or the subscription just added on this phone: the phone reads it later)
export const calConnected = (f?: Feed | null) => !!f && (f.subscribed || !!f.busy || wasAdded())

// the big row — only until something is connected; after that the calendar lives in the header button
export function CalBanner() {
  const feed = useFeed()
  const [open, setOpen] = useState(false)
  if (!feed.data || calConnected(feed.data)) return <CalSheet open={open} onClose={() => setOpen(false)} />
  return (<>
    <button className="calsub rise" onClick={() => setOpen(true)} data-testid="calsub">
      <span className="calsub__ic">📅</span>
      <span className="calsub__t"><b>Подключить календарь телефона</b>
        <span>Встречи появятся там сами, а занятое отметится при выборе времени{feed.data.partner_busy ? ' · у партнёра подключено' : ''}</span></span>
      <span className="calsub__go">›</span>
    </button>
    <CalSheet open={open} onClose={() => { setOpen(false); feed.refresh() }} />
  </>)
}

// the header button: always there, a green dot when both directions work
export function CalButton() {
  const feed = useFeed()
  const [open, setOpen] = useState(false)
  const f = feed.data
  const both = !!f && (f.subscribed || wasAdded()) && !!f.busy
  return (<>
    <button className="iconbtn calbtn" aria-label="Календарь телефона" data-testid="calbtn" onClick={() => setOpen(true)}>
      <Icon n="cal" s={19} />{f && calConnected(f) && <i className={'calbtn__dot' + (both ? '' : ' calbtn__dot--half')} />}
    </button>
    <CalSheet open={open} onClose={() => { setOpen(false); feed.refresh() }} />
  </>)
}

export function CalSheet({ open, onClose, start = 'main' }: { open: boolean; onClose: () => void; start?: 'main' | 'busy' }) {
  const feed = useFeed()
  const [step, setStep] = useState<'main' | 'busy'>(start)
  const [added, setAdded] = useState(wasAdded)
  const f = feed.data
  const ios = /iPhone|iPad|Mac/.test(navigator.userAgent)
  const add = (link: string) => { try { localStorage.setItem(ADDED, String(Date.now())) } catch { /* private */ } setAdded(true); openLink(link) }
  const copy = async (t: string) => { try { await navigator.clipboard.writeText(t); haptic.ok(); toast('Ссылка скопирована') } catch { toast(t) } }
  const close = () => { onClose(); setTimeout(() => setStep(start), 300) }
  const google = f ? 'https://calendar.google.com/calendar/r?cid=' + encodeURIComponent(f.webcal) : ''
  return (
    <Sheet open={open} onClose={close}>
      {step === 'busy' ? <BusyForm connected={!!f?.busy} onDone={() => { feed.refresh(); emit('round'); start === 'busy' ? close() : setStep('main') }} /> : <>
      <h2 className="title" style={{ margin: '0 0 4px' }}>Календарь телефона</h2>
      <p className="callout muted" style={{ margin: '0 0 14px' }}>Две независимые вещи — можно одну, можно обе.</p>
      {!f ? <Skel h={160} /> : <div className="col" style={{ gap: 12 }} data-testid="calsheet">
        <div className="calstep">
          <div className="calstep__h"><b>1. Наши встречи → в телефон</b><Status ok={f.subscribed} wait={!f.subscribed && added} /></div>
          <p className="caption muted" style={{ margin: 0 }}>{f.subscribed ? 'Телефон забирает встречи сам: договорились — появилась, перенесли — поменялась.'
            : added ? 'Подписка добавлена. iPhone подтянет за минуты, Google — до нескольких часов.'
            : 'Договоритесь — встреча появится в календаре сама, перенесёте — поменяется.'}</p>
          {!f.subscribed && <div className="col" style={{ gap: 8 }}>
            {ios ? <Btn kind={added ? 'soft' : 'accent'} block icon="cal" onClick={() => add(f.webcal)}>Добавить в Календарь iPhone</Btn>
              : <Btn kind={added ? 'soft' : 'accent'} block icon="cal" onClick={() => add(google)}>Добавить в Google Календарь</Btn>}
            <div className="row" style={{ gap: 8 }}>
              <Btn kind="surface" sm block onClick={() => add(ios ? google : f.webcal)}>{ios ? 'Google' : 'Другой (webcal)'}</Btn>
              <Btn kind="surface" sm block icon="link" onClick={() => copy(f.url)}>Ссылка</Btn>
            </div>
          </div>}
        </div>
        <div className="calstep">
          <div className="calstep__h"><b>2. Моя занятость ← из телефона</b><Status ok={!!f.busy} /></div>
          <p className="caption muted" style={{ margin: 0 }}>{f.busy ? 'Когда выбираете время, занятые у тебя часы уже отмечены. Партнёр видит только «занято».'
            : 'Чтобы при выборе времени были видны твои занятые часы. Нужна секретная iCal-ссылка — подписка из шага 1 её не даёт.'}</p>
          <Btn kind={f.busy ? 'surface' : 'soft'} block icon="clock" onClick={() => setStep('busy')}>{f.busy ? 'Изменить или отключить' : 'Подключить занятость'}</Btn>
        </div>
        {f.partner_busy && <p className="caption muted" style={{ margin: 0 }}>У партнёра занятость подключена.</p>}
        <details className="howto"><summary>Нет кнопки или не сработало</summary>
          <p>iPhone: Настройки → Календарь → Учётные записи → Новая → Другое → Подписной календарь → вставить ссылку. Google: «Другие календари» → «Добавить по URL».</p>
          <button className="link" onClick={async () => { await post('/calendar/feed/reset', {}); try { localStorage.removeItem(ADDED) } catch { /* private */ } setAdded(false); await feed.refresh(); toast('Старая ссылка больше не работает') }}>Сменить ссылку на встречи</button>
        </details>
      </div>}
      </>}
    </Sheet>
  )
}

const Status = ({ ok, wait }: { ok: boolean; wait?: boolean }) =>
  <span className={'calstat' + (ok ? ' calstat--ok' : wait ? ' calstat--wait' : '')}>{ok ? 'работает' : wait ? 'ждём телефон' : 'не подключено'}</span>

// «Моя занятость»: the private iCal link of my own calendar. Only busy times are read — the partner sees
// «занят(а)», never what it is.
function BusyForm({ connected, onDone }: { connected: boolean; onDone: () => void }) {
  const [url, setUrl] = useState('')
  const [busy, setBusy] = useState(false)
  const save = async () => {
    setBusy(true)
    try { const r = await post<{ busy: number }>('/calendar/busy', { url }); haptic.ok(); toast(`Подключено — вижу ${r.busy} занятых промежутков`); setUrl(''); onDone() }
    catch { toast('Не получилось прочитать календарь — проверь, что это секретная ссылка iCal') } finally { setBusy(false) }
  }
  const off = async () => { await del('/calendar/busy'); onDone(); toast('Отключено') }
  return (<>
    <h2 className="title" style={{ margin: '0 0 6px' }}>Моя занятость</h2>
    <p className="callout muted" style={{ margin: '0 0 12px' }}>Когда выбираете время, занятые у тебя часы отметятся сами, а «Мне подходит любое» их пропустит. Партнёр увидит только «занято» — без названий.</p>
    <div className="col" style={{ gap: 10 }} data-testid="busyform">
      <div className="field"><label>Секретная ссылка на календарь (iCal)</label>
        <input className="input" placeholder="https://calendar.google.com/calendar/ical/…/basic.ics" value={url} onChange={e => setUrl(e.target.value)} /></div>
      <Btn kind="accent" block busy={busy} disabled={!/^(https|webcal):\/\//.test(url.trim())} onClick={save}>Подключить</Btn>
      <details className="howto" open><summary>Где взять ссылку</summary>
        <p><b>Google:</b> calendar.google.com с компьютера → ⚙ Настройки → слева твой календарь → «Секретный адрес в формате iCal» → скопировать.</p>
        <p><b>iPhone (iCloud):</b> Календарь → «Календари» → ⓘ у нужного → «Общий календарь» → «Поделиться ссылкой».</p>
        <p><b>Яндекс:</b> Календарь → ⚙ у календаря → «Экспорт» → ссылка iCal.</p>
        <p><b>Outlook:</b> Настройки → Календарь → «Общие календари» → «Опубликовать» → ссылка ICS.</p>
      </details>
      {connected && <button className="link" onClick={off}>Отключить мою занятость</button>}
    </div>
  </>)
}
