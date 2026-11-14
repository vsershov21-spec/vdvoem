import { useState } from 'react'
import { useLoad, emit } from '../lib/data'
import { post, ApiError, humanError } from '../lib/api'
import { go } from '../lib/router'
import { haptic } from '../lib/tg'
import { ncase } from '../lib/format'
import { say } from './ui'

type Inv = { state: 'ok' | 'expired' | 'used' | 'paired' | 'own' | 'gone'; from: string; partner?: string; ideas: number; asked: boolean }

// «Приглашение»: who calls, what two people do here, one button. The pair is made by «Принять», never
// silently. A link that cannot be accepted says why and leaves one way on.
export function Invite({ code }: { code: string }) {
  const { data, err, refresh } = useLoad<Inv>('/invite/' + encodeURIComponent(code))
  const [busy, setBusy] = useState(false)
  const [asked, setAsked] = useState(false)
  if (!data) return <div className="scr" data-testid="v2-invite">{!!err && <Stop title="Ссылка не работает" text="Попроси партнёра прислать новую." />}</div>
  const accept = async () => {
    setBusy(true)
    try {
      const r = await post<{ moved: number }>(`/invite/${code}/accept`)
      haptic.ok(); emit('home'); emit('ideas'); go('', true)
      say(r.moved > 0 ? 'Вы в паре. Твои идеи переехали' : 'Вы в паре')
    } catch (e) { if (e instanceof ApiError && e.code.startsWith('invite_')) refresh(); else say(humanError(e)) } finally { setBusy(false) }
  }
  const ask = async () => {
    setBusy(true)
    try { await post(`/invite/${code}/ask`); setAsked(true); say(`${data.from} получит просьбу прислать новую ссылку`) }
    catch (e) { if (e instanceof ApiError && e.code.startsWith('invite_')) refresh(); else say(humanError(e)) } finally { setBusy(false) }
  }
  let body
  if (data.state === 'ok') body = <>
    <div className="inv-top">{data.from} зовёт тебя во «Вдвоём»</div>
    <div className="v-card"><b>Что тут делают вдвоём</b><ul className="ul">
      <li>Отмечают, куда хочется. Совпало: назначают встречу.</li>
      <li>Предлагают время и место. Второй отвечает «Да» или «Нет».</li>
      <li>Делят дела: кто что берёт и к какому сроку.</li></ul></div>
    {data.ideas > 0 && <p className="m">Твои идеи переедут в пару вместе с тобой.</p>}
    <button className="b a w" disabled={busy} data-testid="invite-accept" onClick={accept}>Принять приглашение</button>
    <button className="b ghost w" data-testid="invite-later" onClick={() => go('', true)}>Не сейчас</button>
  </>
  else if (data.state === 'expired') body = asked || data.asked
    ? <Stop title="Ссылка устарела" text={`${data.from} получит просьбу прислать новую. Она придёт тебе в Telegram.`} />
    : <div className="v-card stub" data-testid="invite-stop"><h2>Ссылка устарела</h2><p className="m">Приглашение живёт 3 дня и срабатывает один раз.</p>
      <button className="b p w" disabled={busy} data-testid="invite-ask" onClick={ask}>Попросить {ncase(data.from, 'acc')} прислать новую</button></div>
  else if (data.state === 'used') body = <Stop title={data.from + ' уже в паре'} text="Эта ссылка сработала один раз." />
  else if (data.state === 'paired') body = <div className="v-card stub" data-testid="invite-stop"><h2>Ты в паре с {data.partner ? ncase(data.partner, 'ins') : 'партнёром'}</h2>
    <p className="m">Чтобы принять, выйди из неё в настройках.</p>
    <button className="b p w" data-testid="invite-settings" onClick={() => go('settings/pair')}>В настройки</button>
    <button className="b ghost w" onClick={() => go('', true)}>На главную</button></div>
  else if (data.state === 'own') body = <Stop title="Это твоя ссылка" text="Отправь её партнёру: пара появится, когда он примет приглашение." />
  else body = <Stop title="Ссылка не работает" text="Попроси партнёра прислать новую." />
  return <div className="scr" data-testid="v2-invite">{body}</div>
}

const Stop = ({ title, text }: { title: string; text: string }) => (
  <div className="v-card stub" data-testid="invite-stop"><h2>{title}</h2><p className="m">{text}</p>
    <button className="b w" onClick={() => go('', true)}>На главную</button></div>)
