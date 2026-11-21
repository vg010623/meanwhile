// What the panel shows. "blocked" means Claude is waiting on the person
// (a permission dialog or a question).
export type Phase = 'idle' | 'working' | 'blocked' | 'finished' | 'stopped' | 'error'

export type PanelView = {
  phase: Phase
  startedAt: number // ms since the epoch; 0 when idle
  endedAt: number // ms since the epoch; 0 while running
}

// The JSON `meanwhile lesson …` prints (internal/activities/learn/lesson.go).
export type ConceptInfo = {
  id: string
  title: string
  summary: string
  cards: number
  in_progress?: boolean
  done?: boolean
}

export type CardInfo = {
  index: number // 0-based
  total: number
  id: string
  title: string
  body: string // Markdown
  recap?: string
  has_deeper?: boolean // a saved "Go deeper" explanation exists
}

export type Lesson = {
  screen: 'card' | 'picker' | 'complete'
  concept?: ConceptInfo
  card?: CardInfo
  last_time?: string
  unmet?: ConceptInfo[]
  known?: boolean
  unlocked?: ConceptInfo[]
  picks?: ConceptInfo[]
  all?: ConceptInfo[]
  notice?: string
}

// "Go deeper": a conversation about one card, through the person's own
// Claude session, saved in the Go app's database (shared with the
// terminal app).
export type DeepTurn = {
  question: string
  answer: string
  status?: string
  model?: string
  tokens?: number // only for answers from this session
}

export type DeeperView = {
  status: 'idle' | 'loading' | 'consent' | 'asking' | 'done' | 'error'
  conceptId: string
  cardId: string
  model: string
  rootId: number // the saved thread; 0 before the first answer is saved
  turns: DeepTurn[]
  maxFollowUps: number
  pending: string // the question being asked
  error: string
  note: string // e.g. "couldn't save"
}

declare module 'claude-code' {
  interface PluginState {
    'meanwhile-panel': {
      view: PanelView
      lesson: Lesson | null // null until loaded
      lessonError: string // "" when the last call worked
      deeper: DeeperView
      band: boolean // the side panel couldn't be placed: show the strip above the prompt
      autoOpen: boolean // open by itself after 4 s of work (mirrors $.store 'autoOpen')
    }
  }
}
