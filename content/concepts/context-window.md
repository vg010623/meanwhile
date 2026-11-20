---
id: context-window
title: The Context Window
summary: The model's entire working memory—and what happens when it fills up.
prerequisites: [tokens]
track: foundations
order: 30
---

## The model's desk {#the-model-s-desk}
Think of the context window as the size of the model's physical desk. Everything Claude needs to reason about your task—system instructions, `CLAUDE.md`, past messages, file contents, and command outputs—must fit on that desk at the same time.

The model has no separate hard drive it silently queries on its own. If a file, schema, or error message is not currently sitting in the context window, the model effectively does not know it exists.

When Claude makes a wrong assumption about an interface in another file, it is rarely being careless—that file simply wasn't on the desk yet.

> Recap: The context window is the model's entire working memory; anything outside it is invisible.

## What falls off the edge {#what-falls-off-the-edge}
Every model has a hard ceiling on how many tokens fit on its desk at once. As you read files, run test suites, and iterate on fixes, the token count climbs steadily toward that limit.

Once the window fills up, older parts of the conversation must be dropped or compressed to make room for new turns. Early architectural constraints, edge cases you mentioned twenty messages ago, or original file contents can quietly vanish.

If Claude suddenly forgets a rule you gave it at the start of the session, check your context usage—your instruction may have been pushed off the edge.

> Recap: Hitting the token limit forces old history out, causing the model to forget earlier instructions.

## Why long chats drift {#why-long-chats-drift}
Even before hitting the hard token limit, a cluttered desk degrades performance. Failed attempts, stale stack traces, and superseded code snippets stay in the window alongside your current goal.

Because the model pays attention to everything still in context, dead-end approaches from earlier turns continue tugging at its predictions. Quality drops, edits get inconsistent, and the agent starts repeating mistakes.

Starting a fresh session once a subtask is complete—or summarizing key decisions into a clean prompt—keeps the desk clear and keeps Claude sharp.

> Recap: Cluttered context from failed attempts degrades output quality; clear or reset between subtasks.
