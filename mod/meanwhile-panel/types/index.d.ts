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

// "Go deeper": one model call through the person's own Claude session.
export type DeeperView = {
  status: 'idle' | 'consent' | 'asking' | 'done' | 'error'
  cardId: string // the card it is about
  model: string
  text: string
  error: string
  tokens: number // input + output, to show what it used
}

declare module 'claude-code' {
  interface PluginState {
    'meanwhile-panel': {
      view: PanelView
      lesson: Lesson | null // null until loaded
      lessonError: string // "" when the last call worked
      deeper: DeeperView
    }
  }
}
