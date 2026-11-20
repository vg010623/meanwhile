// The meanwhile lesson panel inside Claude Code's own window.
//
// Timing comes from the engine's events: the panel opens after 4 s of work,
// says when Claude needs the person, and how the turn ended (Esc included).
// Lessons and progress come from the Go app: each action runs
// `meanwhile lesson …` and draws the JSON it prints, so progress is saved
// in the same database the terminal app and `meanwhile stats` use.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { ConceptInfo, Lesson, PanelView, Phase } from '../types'

const PANE = 'meanwhile'
const TITLE = 'meanwhile'

// Grace delay: a quick answer never opens the panel.
const GRACE_MS = 4000

const idle: PanelView = { phase: 'idle', startedAt: 0, endedAt: 0 }
const view = atom({ plugin: 'meanwhile-panel', key: 'view' } as const, idle)
const lesson = atom({ plugin: 'meanwhile-panel', key: 'lesson' } as const, null as Lesson | null)
const lessonError = atom({ plugin: 'meanwhile-panel', key: 'lessonError' } as const, '')

// The binary is found on PATH, in ~/go/bin (go install) or Homebrew's bin;
// MEANWHILE_BIN overrides it.
const RUN_LESSON =
  'PATH="$PATH:$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin"; exec "${MEANWHILE_BIN:-meanwhile}" lesson "$@"'

async function setPhase($: EngineInterface, phase: Phase) {
  const now = await $.clock.now()
  await update($, view, v => ({
    phase,
    startedAt: phase === 'working' && v.phase === 'idle' ? now : v.startedAt,
    endedAt: phase === 'finished' || phase === 'stopped' || phase === 'error' ? now : 0,
  }))
}

// runLesson runs one lesson action and stores what to draw.
async function runLesson($: EngineInterface, args: string[]) {
  let message = ''
  try {
    const r = await $.process.run(['/bin/sh', '-c', RUN_LESSON, 'sh', ...args], { timeoutMs: 5000 })
    const parsed = JSON.parse(r.stdout || 'null') as (Lesson & { error?: string }) | null
    if (parsed === null) {
      message = r.stderr.trim() || `meanwhile exited with ${r.exitCode}`
    } else if (parsed.error) {
      message = parsed.error
    } else {
      await update($, lesson, () => parsed)
    }
  } catch (err) {
    message = `couldn't run meanwhile: ${String(err)}`
  }
  await update($, lessonError, () => message)
}

async function openPanel($: EngineInterface) {
  await runLesson($, ['current'])
  await $.ui.open({ id: PANE, title: TITLE })
}

// plain turns a card's Markdown into paragraphs of plain text.
function plain(md: string): string[] {
  return md
    .split(/\n\s*\n/)
    .map(p => p.replace(/\*\*|__|`/g, '').replace(/^\s*[-*]\s+/gm, '• ').trim())
    .filter(p => p !== '')
}

export const register: Register = on => {
  // Module state; a reload starts it over.
  let turnId = ''
  let timer: { cancel: () => void } | undefined
  let isOpen = false
  let wantsOpen = false // the grace delay passed while Claude was blocked
  let blockedBy = '' // tool_use_id of the call that is waiting on the person

  // /meanwhile opens the panel at any time: after closing it by mistake, or
  // for a lesson while Claude isn't working. Asked for, it seats at any width.
  on('session.start', async ($, e, next) => {
    await $.command.register({
      name: 'meanwhile',
      description: 'Open the meanwhile lesson panel',
    })
    return next(e)
  })

  on('command.run', { command: 'meanwhile' }, async $ => {
    const v = await read($, view)
    if (v.phase !== 'working' && v.phase !== 'blocked') {
      await update($, view, () => idle) // no run in progress: plain lesson time
    }
    await openPanel($)
    isOpen = true
    return { text: 'meanwhile panel opened.' }
  })

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
          await openPanel($)
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

  // A verdict of "ask" means a permission dialog is about to be shown.
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
          await openPanel($)
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
    const l = await read($, lesson)
    const failed = await read($, lessonError)
    const act = (...args: string[]) => () => runLesson($, args)
    const close = () => $.ui.close({ id: PANE })

    const header: Record<Phase, string> = {
      idle: 'Lesson time · no Claude run in progress',
      working: '● Claude is working',
      blocked: '⚠ Claude needs your input',
      finished: '✔ Claude finished',
      stopped: '■ Claude stopped (interrupted)',
      error: '✖ Claude stopped with an error',
    }

    // Theme keys, so the colors follow the person's light or dark theme.
    const headerColor: Record<Phase, string | undefined> = {
      idle: 'subtle',
      working: undefined, // the default text color
      blocked: 'warning',
      finished: 'success',
      stopped: 'permission',
      error: 'error',
    }

    // A list of concepts to start, one button each.
    const conceptButtons = (list: ConceptInfo[]) =>
      list.map(c => (
        <Box key={`c-${c.id}`} flexDirection="column">
          <Button key={`start-${c.id}`} onPress={act('start', c.id)}>
            {c.done ? '✓ ' : ''}
            {c.title}
            {c.in_progress ? ' (in progress)' : ''}
          </Button>
          <Text dimColor>  {c.summary}</Text>
        </Box>
      ))

    let body
    if (v.phase === 'blocked') {
      body = <Text>Switch to Claude. Your card will be here when you get back.</Text>
    } else if (failed !== '') {
      body = (
        <Box flexDirection="column">
          <Text bold>Couldn't load your lesson</Text>
          <Text dimColor>{failed}</Text>
          <Text dimColor>Is meanwhile installed? Try `meanwhile lesson current` in a terminal.</Text>
        </Box>
      )
    } else if (l === null) {
      body = <Text dimColor>Loading your lesson…</Text>
    } else if (l.screen === 'card' && l.concept && l.card) {
      const last = l.card.index === l.card.total - 1
      body = (
        <Box flexDirection="column">
          <Text dimColor>
            {l.concept.title} · card {l.card.index + 1} of {l.card.total}
          </Text>
          {l.last_time ? <Text dimColor>Last time: {l.last_time}</Text> : null}
          {l.unmet && l.unmet.length > 0 ? (
            <Text dimColor>Builds on {l.unmet.map(u => u.title).join(', ')} (not done yet)</Text>
          ) : null}
          <Text> </Text>
          <Text bold>{l.card.title}</Text>
          <Text> </Text>
          {plain(l.card.body).map((p, i) => (
            <Box key={`p-${i}`} flexDirection="column">
              <Text>{p}</Text>
              <Text> </Text>
            </Box>
          ))}
          <Box flexDirection="row" gap={2}>
            <Button hotkey="b" dimColor={l.card.index === 0} onPress={act('back')}>
              ‹ Back
            </Button>
            <Button variant="primary" hotkey="n" onPress={act('next')}>
              {last ? 'Finish ✓' : 'Next ›'}
            </Button>
            <Button hotkey="k" dimColor onPress={act('known')}>
              I know this
            </Button>
          </Box>
        </Box>
      )
    } else if (l.screen === 'complete' && l.concept) {
      // A celebration: what was finished, what it opened up, and the next
      // concept as the one obvious button.
      const picks = l.picks ?? []
      const [first, ...rest] = picks
      body = (
        <Box flexDirection="column">
          {l.known ? (
            <Text bold color="success">
              ✓ Marked "{l.concept.title}" as known
            </Text>
          ) : (
            <Text bold color="success">
              🎉 You finished "{l.concept.title}"!
            </Text>
          )}
          {l.unlocked && l.unlocked.length > 0 ? (
            <Text>🔓 Unlocked: {l.unlocked.map(u => u.title).join(', ')}</Text>
          ) : null}
          <Text> </Text>
          {first ? (
            <Box flexDirection="column">
              <Button key={`next-${first.id}`} variant="primary" hotkey="n" onPress={act('start', first.id)}>
                Start: {first.title} ›
              </Button>
              <Text dimColor>  {first.summary}</Text>
              {rest.length > 0 ? (
                <Box flexDirection="column">
                  <Text> </Text>
                  <Text dimColor>Or pick:</Text>
                  {conceptButtons(rest)}
                </Box>
              ) : null}
            </Box>
          ) : (
            <Box flexDirection="column">
              <Text bold>🏆 You've covered everything so far</Text>
              <Text dimColor>Re-read any concept:</Text>
              {conceptButtons(l.all ?? [])}
            </Box>
          )}
        </Box>
      )
    } else {
      const picks = l.picks ?? []
      body = (
        <Box flexDirection="column">
          <Text bold>{picks.length > 0 ? 'Ready for you' : "You've covered everything that's ready"}</Text>
          {picks.length === 0 ? <Text dimColor>Re-read any concept:</Text> : null}
          <Text> </Text>
          {conceptButtons(picks.length > 0 ? picks : (l.all ?? []))}
        </Box>
      )
    }

    return (
      <Box flexDirection="column">
        <Text bold={v.phase !== 'working'} color={headerColor[v.phase]}>
          {header[v.phase]}
        </Text>
        {l?.notice ? <Text dimColor>{l.notice}</Text> : null}
        <Text> </Text>
        {body}
        <Text> </Text>
        <Box flexDirection="row" gap={2}>
          {l?.screen !== 'picker' ? (
            <Button hotkey="p" dimColor onPress={act('pick')}>
              Pick another
            </Button>
          ) : null}
          <Button role="dismiss" hotkey="x" onPress={close}>
            ✕ Close
          </Button>
        </Box>
      </Box>
    )
  })
}
