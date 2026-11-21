// The meanwhile lesson panel inside Claude Code's own window.
//
// Timing comes from the engine's events: the panel opens after 4 s of work,
// says when Claude needs the person, and how the turn ended (Esc included).
// Lessons and progress come from the Go app: each action runs
// `meanwhile lesson …` and draws the JSON it prints, so progress is saved
// in the same database the terminal app and `meanwhile stats` use.
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { ConceptInfo, DeepTurn, DeeperView, Lesson, PanelView, Phase } from '../types'

const PANE = 'meanwhile'
const TITLE = 'meanwhile'

// Grace delay: a quick answer never opens the panel.
const GRACE_MS = 4000

const idle: PanelView = { phase: 'idle', startedAt: 0, endedAt: 0 }
const view = atom({ plugin: 'meanwhile-panel', key: 'view' } as const, idle)
const lesson = atom({ plugin: 'meanwhile-panel', key: 'lesson' } as const, null as Lesson | null)
const lessonError = atom({ plugin: 'meanwhile-panel', key: 'lessonError' } as const, '')
const noDeeper: DeeperView = {
  status: 'idle', conceptId: '', cardId: '', model: '', rootId: 0, turns: [],
  maxFollowUps: 3, pending: '', error: '', note: '',
}

// The first question of every explanation.
const FIRST_QUESTION = 'Explain this card more deeply.'

// One-click follow-ups, so nobody has to type.
const SUGGESTIONS = ['Give me an example', 'Explain it more simply', 'How does this affect my work with Claude Code?']
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

// runMeanwhile runs `meanwhile lesson <args>` and parses its JSON, or says
// what went wrong.
async function runMeanwhile($: EngineInterface, args: string[], stdin?: string): Promise<{ data?: unknown; error?: string }> {
  try {
    const r = await $.process.run(['/bin/sh', '-c', RUN_LESSON, 'sh', ...args], { timeoutMs: 5000, stdin })
    const parsed = JSON.parse(r.stdout || 'null') as ({ error?: string } & Record<string, unknown>) | null
    if (parsed === null) return { error: r.stderr.trim() || `meanwhile exited with ${r.exitCode}` }
    if (typeof parsed.error === 'string') return { error: parsed.error }
    return { data: parsed }
  } catch (err) {
    return { error: `couldn't run meanwhile: ${String(err)}` }
  }
}

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
  const r = await runMeanwhile($, args)
  if (r.data) await update($, lesson, () => r.data as Lesson)
  await update($, lessonError, () => r.error ?? '')
}

// openDeeper shows the card's saved explanation if there is one (no model
// call), else asks for consent the first time and then asks Claude.
async function openDeeper($: EngineInterface) {
  const l = await read($, lesson)
  if (!l || !l.concept || !l.card) return
  const conceptId = l.concept.id
  const cardId = l.card.id
  await update($, deeper, () => ({ ...noDeeper, status: 'loading', conceptId, cardId }))
  const saved = await runMeanwhile($, ['deeper', conceptId, cardId])
  const thread = saved.data as { root_id: number; turns: DeepTurn[]; max_follow_ups: number } | undefined
  const stored = await $.store.get(DEEPER_MODEL)
  const model = typeof stored === 'string' ? stored : ''
  const base = { ...noDeeper, conceptId, cardId, model, maxFollowUps: thread?.max_follow_ups ?? 3 }
  if (thread && thread.turns.length > 0) {
    await update($, deeper, () => ({ ...base, status: 'done', rootId: thread.root_id, turns: thread.turns }))
    return
  }
  if (model === '') {
    await update($, deeper, () => ({ ...base, status: 'consent' }))
    return
  }
  await update($, deeper, () => base)
  await askDeeper($, FIRST_QUESTION, true)
}

// chooseDeeperModel saves the consent; with nothing asked yet it asks.
async function chooseDeeperModel($: EngineInterface, model: string) {
  await $.store.set(DEEPER_MODEL, model)
  const d = await read($, deeper)
  await update($, deeper, () => ({ ...d, model, status: d.turns.length > 0 ? 'done' : d.status }))
  if (d.turns.length === 0) await askDeeper($, FIRST_QUESTION, true)
}

// askDeeper asks one question through the person's own Claude session (no
// tools, no files: only the card and the conversation so far) and saves the
// answer. fresh starts a new thread ("Ask again").
async function askDeeper($: EngineInterface, question: string, fresh: boolean) {
  const l = await read($, lesson)
  const d = await read($, deeper)
  if (!l || !l.concept || !l.card || d.model === '') return
  const q = question.trim()
  if (q === '') return
  deeperAbort?.abort()
  const abort = new AbortController()
  deeperAbort = abort
  const turns = fresh ? [] : d.turns
  const rootId = fresh ? 0 : d.rootId
  await update($, deeper, () => ({ ...d, status: 'asking', turns, rootId, pending: q, error: '', note: '' }))

  let prompt = `Concept: ${l.concept.title}. ${l.concept.summary}

The card the learner is reading, "${l.card.title}":
${l.card.body}
`
  if (turns.length > 0) {
    prompt += '\nEarlier in this conversation:\n' + turns.map(t => `Learner: ${t.question}\nYou: ${t.answer}`).join('\n') + '\n'
  }
  prompt += `\nLearner: ${q}`

  let next: DeeperView
  try {
    const r = await $.model.complete({ model: d.model, system: DEEPER_SYSTEM, prompt, maxTokens: 800 }, { signal: abort.signal })
    if (r.isAnswered) {
      const tokens = r.usage.input_tokens + r.usage.output_tokens
      const turn: DeepTurn = { question: q, answer: r.text, model: d.model, tokens }
      const save = await runMeanwhile(
        $,
        ['deeper-save'],
        JSON.stringify({ concept: d.conceptId, card: d.cardId, root_id: rootId, question: q, answer: r.text, model: d.model }),
      )
      const savedRoot = (save.data as { root_id?: number } | undefined)?.root_id ?? rootId
      next = { ...d, status: 'done', turns: [...turns, turn], rootId: savedRoot, pending: '', error: '', note: save.error ? `Not saved: ${save.error}` : '' }
    } else if (r.reason === 'aborted') {
      return // Stop, or the person moved on
    } else {
      const why = r.reason === 'api-error' ? `Claude answered with an error (${r.status ?? 'no status'}).` : 'Claude sent an empty reply.'
      next = { ...d, status: 'error', turns, rootId, error: why }
    }
  } catch (err) {
    next = { ...d, status: 'error', turns, rootId, error: String(err) }
  }
  if (deeperAbort !== abort) return // a newer call or a close replaced this one
  deeperAbort = undefined
  await update($, deeper, () => next)
}

// stopDeeper cuts a running call and goes back to what was there.
async function stopDeeper($: EngineInterface) {
  deeperAbort?.abort()
  deeperAbort = undefined
  const d = await read($, deeper)
  await update($, deeper, () => (d.turns.length > 0 ? { ...d, status: 'done', pending: '' } : noDeeper))
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
    const { Box, Text, Button, Input } = $.ui.resolve(e)
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
      const followUpsLeft = d.maxFollowUps - Math.max(d.turns.length - 1, 0)
      // The conversation so far: the first answer, then each follow-up.
      const thread = d.turns.map((t, i) => (
        <Box key={`t-${i}`} flexDirection="column">
          {i > 0 ? <Text bold>You: {t.question}</Text> : null}
          {i > 0 ? <Text> </Text> : null}
          {plain(t.answer).map((p, j) => (
            <Box key={`t-${i}-${j}`} flexDirection="column">
              <Text>{p}</Text>
              <Text> </Text>
            </Box>
          ))}
          {t.tokens ? <Text dimColor>Used about {t.tokens} tokens of your Claude usage.</Text> : null}
          {t.tokens ? <Text> </Text> : null}
        </Box>
      ))
      if (d.status === 'loading') {
        body = <Text dimColor>Opening your explanation…</Text>
      } else if (d.status === 'consent') {
        body = (
          <Box flexDirection="column">
            <Text bold>Go deeper with Claude</Text>
            <Text> </Text>
            <Text>Claude explains this card in more depth, and you can ask follow-up questions.</Text>
            <Text> </Text>
            <Text color="warning">
              This uses your own Claude account. On a Pro or Max plan it counts toward your usage limits; with an
              API key it costs API credits.
            </Text>
            <Text dimColor>
              Each answer is one short request with only the card and your conversation about it: no tools, no access
              to your files. Answers are saved on this computer, so reopening one costs nothing.
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
              <Button key="model-no" dimColor onPress={() => (d.turns.length > 0 ? update($, deeper, () => ({ ...d, status: 'done' })) : closeDeeper($))}>
                {d.turns.length > 0 ? 'Keep current' : 'Not now'}
              </Button>
            </Box>
          </Box>
        )
      } else {
        body = (
          <Box flexDirection="column">
            <Text color="claude">
              Going deeper · {l.card.title}
              {d.model ? ` · ${d.model}` : ''}
            </Text>
            <Text> </Text>
            {thread}
            {d.status === 'asking' ? (
              <Box flexDirection="column">
                {d.turns.length > 0 ? <Text bold>You: {d.pending}</Text> : null}
                <Text dimColor>Asking Claude ({d.model})…</Text>
                <Text> </Text>
                <Button key="deeper-stop" onPress={() => stopDeeper($)}>
                  Stop
                </Button>
              </Box>
            ) : null}
            {d.status === 'error' ? <Text color="error">{d.error}</Text> : null}
            {d.note !== '' ? <Text dimColor>{d.note}</Text> : null}
            {d.status === 'done' && followUpsLeft > 0 ? (
              <Box flexDirection="column">
                <Text dimColor>
                  Ask a follow-up ({followUpsLeft} left):
                </Text>
                <Box flexDirection="row" gap={2}>
                  {SUGGESTIONS.map((q, i) => (
                    <Button key={`suggest-${i}`} dimColor onPress={() => askDeeper($, q, false)}>
                      {q}
                    </Button>
                  ))}
                </Box>
                <Input
                  key="ask-own"
                  placeholder="Or type your own question and press Enter"
                  submitLabel="Ask"
                  onSubmit={(value: string) => askDeeper($, value, false)}
                />
              </Box>
            ) : null}
            {d.status === 'done' && followUpsLeft <= 0 ? (
              <Text dimColor>That's all the follow-ups for this explanation. "Ask again" starts a fresh one.</Text>
            ) : null}
            <Text> </Text>
            <Box flexDirection="row" gap={2}>
              {back}
              {d.status === 'error' ? (
                <Button key="deeper-retry" onPress={() => askDeeper($, d.pending || FIRST_QUESTION, d.turns.length === 0)}>
                  Try again
                </Button>
              ) : null}
              {d.status === 'done' ? (
                <Button key="deeper-again" dimColor onPress={() => askDeeper($, FIRST_QUESTION, true)}>
                  Ask again
                </Button>
              ) : null}
              {d.status !== 'asking' ? (
                <Button key="deeper-model" dimColor onPress={() => update($, deeper, () => ({ ...d, status: 'consent' }))}>
                  Change model
                </Button>
              ) : null}
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
              {l.card.has_deeper ? '📖 Your explanation' : 'Go deeper'}
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
