// What the panel shows. "blocked" means Claude is waiting on the person
// (a permission dialog or a question).
export type Phase = 'idle' | 'working' | 'blocked' | 'finished' | 'stopped' | 'error'

export type PanelView = {
  phase: Phase
  startedAt: number // ms since the epoch; 0 when idle
  endedAt: number // ms since the epoch; 0 while running
}

declare module 'claude-code' {
  interface PluginState {
    'meanwhile-panel': { view: PanelView; card: number }
  }
}
