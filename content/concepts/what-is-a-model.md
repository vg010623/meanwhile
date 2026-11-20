---
id: what-is-a-model
title: What Is a Model?
summary: A giant function learned from data rather than written by hand.
prerequisites: []
track: foundations
order: 10
---

## A function you can't read {#a-function-you-can-t-read}
In normal software, you write explicit rules: if the input looks like this, return that. A large language model is still just a function—text goes in, text comes out—except nobody wrote its `if` statements.

Instead, the logic lives inside billions of tuned numbers called weights. You cannot open a source file to inspect why it chose a particular variable name or refactor strategy, because the behavior is spread across all those numbers at once.

When using Claude Code, treat the model like a black-box library with an empirical contract. You steer it through clear inputs and constraints, not by assuming there is a deterministic rule engine underneath.

> Recap: An LLM is a text-in, text-out function whose logic lives in billions of weights, not readable code.

## Learned, not written {#learned-not-written}
Traditional compilers and linters follow formal grammars written by engineers. An LLM acquires its behavior by compressing statistical patterns from trillions of words of public code, documentation, and prose.

During training, the system repeatedly adjusted its internal weights to get better at predicting missing text. Over time, those adjustments encoded syntax rules, common design patterns, library APIs, and debugging idioms—without anyone programming a single parser.

Because those patterns come from real repositories, Claude writes idiomatic code effortlessly for popular stacks, yet can stumble on private internal frameworks or brand-new releases it rarely saw during training.

> Recap: Models learn coding patterns from massive training data rather than hand-coded syntax rules.

## Why it guesses {#why-it-guesses}
A compiler checks truth against a language specification. A language model has no built-in truth checker; it only calculates which continuation looks most plausible given the patterns it learned.

When you ask for a standard React hook or SQL query, the statistical pattern is overwhelming, so the output looks rock-solid. When you ask about an obscure CLI flag, the model still produces a confident-looking guess rather than throwing a compile error.

In daily work, this means fluency is never proof of correctness. Always pair Claude's confident code edits with typechecks, tests, or a quick read of the diff.

> Recap: Models optimize for plausible continuations, not verified truth—always validate output with tests.
