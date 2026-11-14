import type { Home } from '../lib/types'

export type InboxRow = { id: number; kind: 'time' | 'stop' | 'chore' | 'cancel' | 'note'; ref_id: number; role: 'answer' | 'declined' | 'stale' | 'notice' | 'waiting'; title: string; sub: string; hot: boolean; actions: string[]; created: number }
export type ChoreSummary = { today: number; overdue: number; titles: string[] }
export type HomeV2 = Home & { inbox?: InboxRow[]; chore_summary?: ChoreSummary }
