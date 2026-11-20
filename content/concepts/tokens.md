---
id: tokens
title: Tokens
summary: How text gets chopped into numeric chunks before the model sees it.
prerequisites: [what-is-a-model]
track: foundations
order: 20
---

## Text becomes numbers {#text-becomes-numbers}
Models cannot read raw strings or characters directly. Before Claude processes your prompt or codebase, a lightweight preprocessing step called a tokenizer slices the text into chunks and maps each chunk to an integer ID.

On the way out, the process reverses: the model outputs a stream of integer IDs, and the tokenizer decodes them back into readable source code and prose.

Because the model only ever sees those numeric IDs, tasks that depend on exact character positions—like counting letters in a string or aligning ASCII tables—are surprisingly awkward for it compared to writing logic.

> Recap: A tokenizer converts text into integer IDs on input and back into text on output.

## Why not whole words? {#why-not-whole-words}
Splitting on spaces fails quickly in code: `getUserById`, `snake_case_var`, and `!==` would create an impossibly huge vocabulary, while splitting into single characters makes sequences way too long to process efficiently.

Tokenizers compromise with subwords. Common words and keywords like `function` or `return` become single tokens, while rarer identifiers split into familiar pieces like `get`, `User`, `By`, and `Id`. Whitespace and indentation also consume tokens.

This explains why verbose formatting, deeply nested indentation, or minified lockfiles burn through budget fast—every extra structural symbol still gets tokenized and processed.

> Recap: Subword tokens balance vocabulary size and sequence length, splitting rare identifiers into pieces.

## Tokens cost money {#tokens-cost-money}
Every token entering the model and every token generated back is metered. Input tokens cover your prompt, conversation history, system rules, and every file Claude reads; output tokens cover its replies, tool calls, and file edits.

Output tokens typically cost several times more than input tokens because generating them requires a full forward pass per token, whereas input tokens can be processed in parallel.

When you ask Claude Code to "inspect the whole repo" or dump a 5,000-line log file into chat, you pay for every single token in that payload—and keep paying for it on subsequent turns.

> Recap: Both input and output tokens are metered; huge file reads and verbose logs directly inflate cost.
