// The meanwhile lesson panel inside Claude Code's own window.
//
// Timing comes from the engine's events: the panel opens after 4 s of work,
// says when Claude needs the person, and how the turn ended (Esc included).
// Lessons and progress come from the Go app: each action runs
// `meanwhile lesson …` and draws the JSON it prints, so progress is saved
// in the same database the terminal app and `meanwhile stats` use.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { ConceptInfo, DeeperView, Lesson, PanelView, Phase } from '../types'

const PANE = 'meanwhile'
const TITLE = 'meanwhile'

// Grace delay: a quick answer never opens the panel.
const GRACE_MS = 4000

const idle: PanelView = { phase: 'idle', startedAt: 0, endedAt: 0 }
const view = atom({ plugin: 'meanwhile-panel', key: 'view' } as const, idle)
const lesson = atom({ plugin: 'meanwhile-panel', key: 'lesson' } as const, null as Lesson | null)
const lessonError = atom({ plugin: 'meanwhile-panel', key: 'lessonError' } as const, '')
const noDeeper: DeeperView = { status: 'idle', cardId: '', model: '', text: '', error: '', tokens: 0 }
const deeper = atom({ plugin: 'meanwhile-panel', key: 'deeper' } as const, noDeeper)

// $.store key (kept across sessions): the model the person chose on the
// Go deeper consent screen. Unset means they haven't agreed yet.
const DEEPER_MODEL = 'deeperModel'

// The same tutor prompt the terminal app's Go deeper uses.
const DEEPER_SYSTEM = `You are a patient tutor inside "meanwhile", a terminal app that teaches AI concepts to software developers while they wait for their coding agent.

Explain from first principles, in plain language:
- Build on what the learner already knows; they are a developer, not an ML researcher.
- Explain why the idea exists and what problem it solves, not just what it is.
- Use one concrete analogy or example.
- No math notation and no code unless the learner asks for it.
- Keep it under about 250 words, in short paragraphs. Light Markdown only (bold, bullet lists).
You have no tools and cannot see any files. Never offer to run, open or look at anything.`

// The running Go deeper call, so Stop (or leaving the card) can cut it.
let deeperAbort: AbortController | undefined

// The binary is found on PATH, in ~/.local/bin, ~/go/bin (go install) or Homebrew's bin;
// MEANWHILE_BIN overrides it.
const RUN_LESSON =
  'PATH="$PATH:$HOME/.local/bin:$HOME/go/bin:/opt/homebrew/bin:/usr/local/bin"; exec "${MEANWHILE_BIN:-meanwhile}" lesson "$@"'

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
  await closeDeeper($) // a new screen: any explanation belonged to the old one
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

// openDeeper asks for consent the first time, then explains the card.
async function openDeeper($: EngineInterface) {
  const model = await $.store.get(DEEPER_MODEL)
  if (typeof model !== 'string' || model === '') {
    await update($, deeper, () => ({ ...noDeeper, status: 'consent' }))
    return
  }
  await askDeeper($, model)
}

async function chooseDeeperModel($: EngineInterface, model: string) {
  await $.store.set(DEEPER_MODEL, model)
  await askDeeper($, model)
}

// askDeeper makes one model call through the person's own Claude session:
// no tools, no files, only the card's text.
async function askDeeper($: EngineInterface, model: string) {
  const l = await read($, lesson)
  if (!l || !l.concept || !l.card) return
  deeperAbort?.abort()
  const abort = new AbortController()
  deeperAbort = abort
  const cardId = l.card.id
  await update($, deeper, () => ({ ...noDeeper, status: 'asking', cardId, model }))
  const prompt = `Concept: ${l.concept.title}. ${l.concept.summary}

The card the learner is reading, "${l.card.title}":
${l.card.body}

Learner: Explain this card more deeply.`
  let result: DeeperView
  try {
    const r = await $.model.complete({ model, system: DEEPER_SYSTEM, prompt, maxTokens: 800 }, { signal: abort.signal })
    if (r.isAnswered) {
      const tokens = r.usage.input_tokens + r.usage.output_tokens
      result = { ...noDeeper, status: 'done', cardId, model, text: r.text, tokens }
    } else if (r.reason === 'aborted') {
      return // Stop, or the person moved on
    } else {
      const why = r.reason === 'api-error' ? `Claude answered with an error (${r.status ?? 'no status'}).` : 'Claude sent an empty reply.'
      result = { ...noDeeper, status: 'error', cardId, model, error: why }
    }
  } catch (err) {
    result = { ...noDeeper, status: 'error', cardId, model, error: String(err) }
  }
  if (deeperAbort !== abort) return // a newer call or a close replaced this one
  deeperAbort = undefined
  await update($, deeper, () => result)
}

async function closeDeeper($: EngineInterface) {
  deeperAbort?.abort()
  deeperAbort = undefined
  await update($, deeper, () => noDeeper)
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

// Module state; a reload starts it over.
let turnId = ''
let timer: { cancel: () => void } | undefined
let isOpen = false
let wantsOpen = false // the grace delay passed while Claude was blocked
// What Claude is waiting on the person for: an AskUserQuestion call's
// tool_use_id, PERMISSION for a permission dialog, or '' for nothing.
let blockedBy = ''
const PERMISSION = 'permission-dialog'

// unblock goes back to "working" once the person has answered.
async function unblock($: EngineInterface) {
  blockedBy = ''
  const v = await read($, view)
  if (v.phase !== 'blocked') return
  await setPhase($, 'working')
  if (wantsOpen && !isOpen) {
    wantsOpen = false
    await openPanel($)
    isOpen = true
  }
}

export const register: Register = on => {

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

  // A permission dialog is about to be shown. (Not tool.check's "ask": in
  // auto mode most asks are approved by the classifier with no dialog, and
  // the panel wrongly said "needs your input" for every shell command.)
  on('classic.PermissionRequest', async ($, e, next) => {
    if (!turnId) return next(e)
    blockedBy = PERMISSION
    await setPhase($, 'blocked')
    const started = await $.clock.now()
    const answer = await next(e)
    const waited = (await $.clock.now()) - started
    // If the chain waited for the dialog, the person has answered: resume
    // now rather than when the command finishes.
    if (waited > 1500 && blockedBy === PERMISSION) await unblock($)
    return answer
  })

  on('tool.call', async ($, e, next) => {
    if (e.tool === 'AskUserQuestion' && turnId) {
      blockedBy = e.tool_use_id
      await setPhase($, 'blocked')
    }
    const result = await next(e)
    if (blockedBy !== '' && (blockedBy === e.tool_use_id || blockedBy === PERMISSION)) {
      await unblock($)
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
    const d = await read($, deeper)
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
    } else if (l.screen === 'card' && l.concept && l.card && d.status !== 'idle') {
      const back = (
        <Button key="deeper-back" hotkey="b" onPress={() => closeDeeper($)}>
          ‹ Back to card
        </Button>
      )
      if (d.status === 'consent') {
        body = (
          <Box flexDirection="column">
            <Text bold>Go deeper with Claude</Text>
            <Text> </Text>
            <Text>Claude explains this card in more depth, building on what you've read.</Text>
            <Text> </Text>
            <Text color="warning">
              This uses your own Claude account. On a Pro or Max plan it counts toward your usage limits; with an
              API key it costs API credits.
            </Text>
            <Text dimColor>
              Each answer is one short request with only the card's text: no tools, no access to your files.
            </Text>
            <Text> </Text>
            <Text>Which model should it use? (Remembered; you can change it later.)</Text>
            <Box flexDirection="row" gap={2}>
              <Button key="model-haiku" variant="primary" onPress={() => chooseDeeperModel($, 'haiku')}>
                Haiku · uses the least
              </Button>
              <Button key="model-sonnet" onPress={() => chooseDeeperModel($, 'sonnet')}>
                Sonnet · deeper answers
              </Button>
              <Button key="model-no" dimColor onPress={() => closeDeeper($)}>
                Not now
              </Button>
            </Box>
          </Box>
        )
      } else if (d.status === 'asking') {
        body = (
          <Box flexDirection="column">
            <Text dimColor>
              Asking Claude ({d.model}) about "{l.card.title}"…
            </Text>
            <Text> </Text>
            <Button key="deeper-stop" onPress={() => closeDeeper($)}>
              Stop
            </Button>
          </Box>
        )
      } else {
        body = (
          <Box flexDirection="column">
            <Text color="claude">
              Going deeper · {l.card.title} · {d.model}
            </Text>
            <Text> </Text>
            {d.status === 'error' ? (
              <Text color="error">{d.error}</Text>
            ) : (
              plain(d.text).map((p, i) => (
                <Box key={`d-${i}`} flexDirection="column">
                  <Text>{p}</Text>
                  <Text> </Text>
                </Box>
              ))
            )}
            {d.status === 'done' ? (
              <Text dimColor>Used about {d.tokens} tokens of your Claude usage.</Text>
            ) : null}
            <Text> </Text>
            <Box flexDirection="row" gap={2}>
              {back}
              {d.status === 'error' ? (
                <Button key="deeper-retry" onPress={() => askDeeper($, d.model)}>
                  Try again
                </Button>
              ) : null}
              <Button
                key="deeper-model"
                dimColor
                onPress={() => update($, deeper, () => ({ ...noDeeper, status: 'consent' }))}
              >
                Change model
              </Button>
            </Box>
          </Box>
        )
      }
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
            <Button hotkey="d" onPress={() => openDeeper($)}>
              Go deeper
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
