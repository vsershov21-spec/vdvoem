import type { Person } from '../lib/types'
import { SubHeader } from '../ui/kit'
import { Deck } from '../ui/Deck'

// «Разобрать всё» as its own screen (from the bot and old links): the same deck as on the main screen
export function Triage({ partner }: { partner?: Person }) {
  return (<>
    <SubHeader title="Выбрать" />
    <Deck partner={partner} />
  </>)
}
