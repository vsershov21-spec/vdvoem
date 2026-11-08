export type Wish = '' | 'skip' | 'want' | 'very'
export type Person = { name: string; color: 'a' | 'b'; photo?: string }

export type Idea = {
  id: number; title: string; kind: 'leisure' | 'chore'; category: string
  url?: string; address?: string; image?: string; note?: string
  price_min?: number; price_max?: number; duration_min?: number
  starts_at?: number; ends_at?: number; deadline?: number; assignee?: string
  status: string; my_wish: Wish; partner_marked: boolean; partner_wish?: Wish
  mine: boolean; parsed: boolean; meta: string; liked_before?: boolean; created_at: number
  maps: string; generic: boolean; options?: PlaceOption[]; place?: { title: string; address?: string; url?: string; maps: string; chosen: boolean }
  source?: string; why?: string; horizon?: 'week' | 'later'; on_sale?: number
  match?: { me: number; partner: number; both: number; note: string }
}
export type PlaceOption = { id: number; title: string; url?: string; address?: string; image?: string; note?: string; price?: number; author: 'a' | 'b' | 'ai'; chosen: boolean; parsed: boolean; maps: string }

export type RoundState = 'time' | 'vote' | 'confirm' | 'planned' | 'done' | 'expired' | 'cancelled'

export type Round = {
  id: number; state: RoundState; version: number; opened_at: number; expires_at: number
  days: string[]; today?: string; horizon?: string; my_slots: string[]; sent: boolean; partner_sent: boolean
  voted: boolean; partner_voted: boolean; my_vote?: number; partner_vote?: number
  confirmed: boolean; partner_confirmed: boolean; again: string; budget: number
  windows?: { from: number; to: number }[]; triple: Idea[]; chosen?: Idea
  starts_at?: number; ends_at?: number; when?: string; compromise?: Idea; photos?: number
  route?: RouteStop[]; extra_suggest?: Idea[]
}

export type RouteStop = { id: number; idea: Idea; starts_at: number; ends_at: number; state: 'proposed' | 'accepted'; mine: boolean; tight: boolean }

export type Home = {
  me: Person; partner?: Person; paired: boolean
  counts: { ideas: number; chores: number; both_want: number; unmarked: number }
  to_mark: Idea[]; round?: Round; next?: Round
}

export type Card = { type: 'idea' | 'meeting'; idea_id?: number; round_id?: number; title: string; meta?: string; image?: string; category?: string; why?: string; when?: string }
export type ActionBtn = { id: string; label: string; kind: string }
export type Reply = { text: string; cards?: Card[]; actions?: ActionBtn[] }
export type ChatMsg = { id: number; role: 'user' | 'assistant'; at: number; reply: Reply }

export type Settings = {
  city: string; tz: string; plan_weekday: number; plan_hour: number; budget: number
  suggest_mode: 'best' | 'random' | 'mix' | 'off'; suggest_weekday: number; suggest_hour: number; paired: boolean
}

export type Stats = {
  months: { month: string; label: string; done: number; planned: number }[]
  meetings: number; hours: number; spent: number; ideas: number; both_want: number; both_marked: number
  match: number; liked: number; unmarked: number; this_month: number; week: number[]; categories: Record<string, number>
}
export type CalEvent = { date: string; kind: 'planned' | 'done' | 'deadline' | 'ends' | 'open'; title: string; time?: string; at?: number; round_id?: number; idea_id?: number; category?: string }
