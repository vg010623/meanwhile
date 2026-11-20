---
id: context-engineering
title: Context Engineering
summary: Curating the exact files, errors, and constraints the model needs right now.
prerequisites: [context-window]
track: using-agents-well
order: 170
---

## Signal over noise {#signal-over-noise}
Developers often spend energy tweaking polite phrasing while dumping noisy, unfocused context into the session. In practice, what is sitting in the context window matters far more than clever wording.

Context engineering means deliberately choosing which files, type definitions, and reproduction steps enter the window—and keeping irrelevant files out. Too little context forces the model to guess; too much irrelevant code dilutes its focus.

Before asking Claude Code for a complex change, point it directly at the relevant entry point, types, and existing tests instead of telling it to figure out the codebase from scratch.

> Recap: Curating high-signal files and types beats clever prompt wording every time.

## Front-load constraints {#front-load-constraints}
Every back-and-forth correction adds both the wrong attempt and your correction to the context window, doubling the clutter. Stating non-negotiable constraints upfront prevents the model from walking down the wrong path in the first place.

Include key boundaries right in your initial request or `CLAUDE.md`: target runtime, forbidden dependencies, backward-compatibility rules, and where tests live.

A single sentence like "Do not add new packages; follow the repository pattern in `src/db/users.ts`" saves three rounds of cleanup and keeps the context window clean.

> Recap: State invariants, patterns, and prohibitions upfront to prevent wasted turns and context bloat.

## Curate tool output {#curate-tool-output}
In an agentic session, you are not the only one filling the context window—every shell command and search Claude runs dumps its output straight back into context.

Running an unfiltered test suite that prints 1,500 lines of passing logs or grepping across `node_modules` can consume huge chunks of your window in seconds with zero useful signal.

Configure project ignore rules, ask Claude to run targeted single-file tests during iteration, and paste only the relevant stack trace frames rather than entire build logs.

> Recap: Unfiltered test and search outputs rapidly eat context; scope commands to return only signal.
