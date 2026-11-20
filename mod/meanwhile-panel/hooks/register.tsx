// M7 prototype: a meanwhile lesson panel inside Claude Code's own window.
// Static text only; the Go app is not called yet. The point is to confirm
// the events: when a turn starts, when Claude waits on the person, and how
// a turn ends (answer, Esc, error).
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { PanelView, Phase } from '../types'

const PANE = 'meanwhile'
const TITLE = 'meanwhile'

// Grace delay: a quick answer never opens the panel.
const GRACE_MS = 4000

const idle: PanelView = { phase: 'idle', startedAt: 0, endedAt: 0 }
const view = atom({ plugin: 'meanwhile-panel', key: 'view' } as const, idle)
const card = atom({ plugin: 'meanwhile-panel', key: 'card' } as const, 0)

// Placeholder cards until the panel reads real lessons from the Go app.
const CARDS = [
  { title: 'A function you can\'t read', body: 'PLACEHOLDER card 1. A model is a function from input to output whose rules were learned from examples, not written by a person.' },
  { title: 'Weights', body: 'PLACEHOLDER card 2. The learned rules live in billions of numbers called weights. Training nudges them until the outputs look right.' },
  { title: 'Why it matters', body: 'PLACEHOLDER card 3. Because nobody wrote the rules, nobody can read them off either: you learn what a model does by testing it.' },
]

async function setPhase($: EngineInterface, phase: Phase) {
  const now = await $.clock.now()
  await update($, view, v => ({
    phase,
    startedAt: phase === 'working' && v.phase === 'idle' ? now : v.startedAt,
    endedAt: phase === 'finished' || phase === 'stopped' || phase === 'error' ? now : 0,
  }))
}

export const register: Register = on => {
  // Module state; a reload starts it over, which is fine for a prototype.
  let turnId = ''
  let timer: { cancel: () => void } | undefined
  let isOpen = false
  let wantsOpen = false // the grace delay passed while Claude was blocked
  let blockedBy = '' // tool_use_id of the call that is waiting on the person

  on('turn.start', async ($, e, next) => {
    const now = await $.clock.now()
    turnId = e.turnId
    wantsOpen = false
    blockedBy = ''
    timer?.cancel()
    await update($, view, () => ({ phase: 'working', startedAt: now, endedAt: 0 }))
    const mine = e.turnId
    timer = $.clock.after(GRACE_MS, () => {
      void (async () => {
        if (turnId !== mine) return
        const v = await read($, view)
        if (v.phase === 'working') {
          await $.ui.open({ id: PANE, title: TITLE })
          isOpen = true
        } else if (v.phase === 'blocked') {
          wantsOpen = true
        }
      })()
    })
    return next(e)
  })

  on('turn.complete', async ($, e, next) => {
    if (e.agentId || e.turnId !== turnId) return next(e) // a subagent's turn
    timer?.cancel()
    timer = undefined
    blockedBy = ''
    if (isOpen) {
      await setPhase($, e.reason === 'answer' ? 'finished' : e.reason === 'aborted' ? 'stopped' : 'error')
    } else {
      await update($, view, () => idle) // a quick answer: never shown
    }
    return next(e)
  })

  // A verdict of "ask" means a permission dialog is about to be shown
  // (unless the mode's decider settles it; to be checked live).
  on('tool.check', async ($, e, next) => {
    const verdict = await next(e)
    if (verdict.decision === 'ask' && e.tool_use_id && turnId) {
      blockedBy = e.tool_use_id
      await setPhase($, 'blocked')
    }
    return verdict
  })

  on('tool.call', async ($, e, next) => {
    if (e.tool === 'AskUserQuestion' && turnId) {
      blockedBy = e.tool_use_id
      await setPhase($, 'blocked')
    }
    const result = await next(e)
    if (blockedBy !== '' && blockedBy === e.tool_use_id) {
      blockedBy = ''
      const v = await read($, view)
      if (v.phase === 'blocked') {
        await setPhase($, 'working')
        if (wantsOpen && !isOpen) {
          wantsOpen = false
          await $.ui.open({ id: PANE, title: TITLE })
          isOpen = true
        }
      }
    }
    return result
  })

  on('ui.close', async ($, e, next) => {
    if (e.id === PANE) isOpen = false
    return next(e)
  })

  on('ui.render', { component: 'Pane', requestId: PANE }, async ($, e) => {
    const { Box, Text, Button } = $.ui.resolve(e)
    const v = await read($, view)
    const i = await read($, card)
    const close = () => $.ui.close({ id: PANE })
    const back = () => update($, card, n => Math.max(n - 1, 0))
    const forward = () => update($, card, n => Math.min(n + 1, CARDS.length - 1))

    const header: Record<Phase, string> = {
      idle: 'Lesson time · no Claude run in progress',
      working: '● Claude is working',
      blocked: '⚠ Claude needs your input',
      finished: '✔ Claude finished',
      stopped: '■ Claude stopped (interrupted)',
      error: '✖ Claude stopped with an error',
    }

    return (
      <Box flexDirection="column">
        <Text bold={v.phase !== 'working'} dimColor={v.phase === 'working'}>
          {header[v.phase]}
        </Text>
        <Text> </Text>
        {v.phase === 'blocked' ? (
          <Text>Switch to Claude. Your card will be here when you get back.</Text>
        ) : (
          <Box flexDirection="column">
            <Text dimColor>What is a model · card {i + 1} of {CARDS.length}</Text>
            <Text bold>{CARDS[i].title}</Text>
            <Text> </Text>
            <Text>{CARDS[i].body}</Text>
          </Box>
        )}
        <Text> </Text>
        <Box flexDirection="row" gap={2}>
          <Button hotkey="b" dimColor={i === 0} onPress={back}>
            ‹ Back
          </Button>
          <Button variant="primary" hotkey="n" autoFocus onPress={forward}>
            Next ›
          </Button>
          <Button role="dismiss" hotkey="x" onPress={close}>
            ✕ Close
          </Button>
        </Box>
        <Text dimColor>Click a button, or ctrl+x tab to focus here, then n / b / x. Esc returns to the prompt.</Text>
      </Box>
    )
  })
}
