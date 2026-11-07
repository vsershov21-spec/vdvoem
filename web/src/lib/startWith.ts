import { post, ApiError, humanError } from './api'
import { emit } from './data'
import { go } from './router'
import { tg } from './tg'
import { toast } from '../ui/kit'
import type { Round } from './types'

const ask = (text: string) => new Promise<boolean>(res => {
  if (tg?.showConfirm && tg.isVersionAtLeast?.('6.2')) { try { tg.showConfirm(text, (ok: boolean) => res(!!ok)); return } catch { /* fall through */ } }
  res(window.confirm(text))
})

// «Идём сюда» / «Давай!»: start a round with this idea. If a choice is already going on,
// offer to put this idea into it instead (marked time is kept) — no dead end.
export async function startWith(id: number, title: string): Promise<boolean> {
  try {
    const r = await post<{ round: Round }>('/rounds/with/' + id)
    emit('home'); go('round/' + r.round.id); return true
  } catch (e) {
    if (!(e instanceof ApiError && e.code === 'round_open')) { toast(humanError(e)); return false }
  }
  if (!(await ask(`У вас уже идёт выбор. Взять «${title}» вместо него? Отмеченное время сохранится.`))) return false
  try {
    const r = await post<{ round: Round }>('/rounds/with/' + id + '?replace=1')
    emit('home'); go('round/' + r.round.id); return true
  } catch (e) { toast(humanError(e)); return false }
}
