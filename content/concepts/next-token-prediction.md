---
id: next-token-prediction
title: Next-Token Prediction
summary: Why generating code is just predicting one piece at a time.
prerequisites: [tokens]
track: how-models-work
order: 40
---

## One piece at a time {#one-piece-at-a-time}
When an LLM writes a 50-line function, it does not design the whole file in its head first and paste it out. It predicts one token, appends that token to the input, and runs the entire calculation again for the next token.

Think of it like autocomplete on steroids. Every single keyword, bracket, and variable name is chosen based only on everything that came before it—both your prompt and the tokens the model just generated a millisecond ago.

Because each step depends on the previous output, early choices heavily steer the rest of the response.

> Recap: Models generate code sequentially, one token at a time, conditioning each guess on everything prior.

## No backspace key {#no-backspace-key}
Once a model emits a token in its answer, it cannot silently delete it and start over (it can rethink only in its private thinking, before the answer starts). If it starts writing a nested `for` loop when a hash map would have been cleaner, it has to finish that loop coherently.

The model is trained to make the upcoming text look like a natural continuation of whatever already exists. That means it will often write convoluted glue code just to justify a shaky assumption it made three lines earlier.

When you see Claude Code heading down an overcomplicated path, hit Escape early instead of waiting for it to finish a bad implementation.

> Recap: Models cannot backtrack on emitted tokens; interrupt early if the initial direction looks wrong.

## Why structure helps {#why-structure-helps}
Because every predicted token becomes context for the next one, giving the model clear scaffolding changes its predictions dramatically.

Asking for "a clean API endpoint" forces the model to invent architecture while simultaneously writing syntax. Asking it to first outline the types or plan the steps lets it lay down strong anchor tokens before writing implementation code.

In Claude Code, using Plan Mode or asking for function signatures before bodies gives the model better previous tokens to predict from.

> Recap: Writing plans, types, or signatures first creates strong anchor tokens that improve downstream code.
