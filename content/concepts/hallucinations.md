---
id: hallucinations
title: Hallucinations
summary: Why models invent plausible-sounding APIs and flags with total confidence.
prerequisites: [next-token-prediction]
track: how-models-work
order: 90
---

## Plausible beats true {#plausible-beats-true}
A model's only job is to predict the most likely next token. Its weights hold a lot of knowledge, but it has no fact table it can check its answer against, and no boolean flag that flips to `false` when it does not know.

If you ask for a CLI flag to export logs in a specific tool, the model sees patterns like `--export-format` or `--output-json` across thousands of other tools. Predicting a flag that *looks* right is statistically natural, even if that tool never implemented it.

Hallucinations are not a glitch—they are the exact same pattern-completion engine working without enough grounding.

> Recap: Hallucinations happen because models predict statistically plausible syntax, not verified facts.

## Confident nonsense {#confident-nonsense}
In human conversation, hesitation signals uncertainty. In next-token prediction, confident prose is simply the most common style found in technical documentation and Stack Overflow answers.

As a result, an LLM uses the exact same authoritative tone when explaining `Array.prototype.map` as it does when inventing a non-existent method on a niche SDK. Tone never correlates with accuracy.

Whether you are vibe-coding a quick prototype or refactoring production services, never treat a clean explanation as proof that an API actually exists.

> Recap: Fluent, authoritative tone comes from training data style and never guarantees factual accuracy.

## How to catch it {#how-to-catch-it}
You cannot prompt a model into never hallucinating, but you can force it to ground its predictions in real artifacts inside your workspace.

Instead of asking Claude Code how a library works from memory, point it to your `package.json`, `go.mod`, or `node_modules` type definitions so those real tokens sit in context. Even better, make it run the compiler or test suite immediately after editing.

Compilers and runtime errors do not care how plausible a hallucinated method name looks.

> Recap: Ground the model in local source files and run compilers or tests to catch invented APIs fast.
