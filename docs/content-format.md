# Writing curriculum

Each concept is one Markdown file in `content/concepts/`, named after its ID:
`content/concepts/tokens.md` holds the concept `tokens`.

## Shape of a file

```markdown
---
id: tokens
title: Tokens
summary: How models chop text into pieces before they ever "read" it.
prerequisites: [what-is-a-model]
track: foundations
order: 20
---

## Text becomes numbers {#text-becomes-numbers}
The card body. Plain Markdown.

> Recap: Models never see letters, only token IDs.

## Why not whole words? {#why-not-words}
...
```

## Front matter

| Field | Required | Meaning |
|---|---|---|
| `id` | yes | Lowercase letters, digits and hyphens. Must match the file name. |
| `title` | yes | Shown in the picker. |
| `summary` | yes | One line shown under the title in the picker. |
| `prerequisites` | no | IDs of concepts that should come first. The app recommends; it never blocks. |
| `track` | no | Groups concepts in the "browse all" view, such as `foundations`. |
| `order` | no | Tiebreak when recommending concepts. Lower comes first. |

Misspelled fields (for example `prerequisite:`) are reported as errors.

## Cards

- Every `## ` heading starts a new card. A concept has **3 to 5 cards**.
- Aim for **60 to 140 words** per card, which is roughly 20 to 40 seconds of reading.
- Add a stable ID to each heading with `{#some-id}`. Saved progress is stored by
  this ID, so readers keep their place if you later insert or reorder cards.
  Without one, the ID is made from the heading text, and editing the heading
  then resets readers' place in that concept.
- A `> Recap:` line is the one-line summary shown when a reader resumes ("Last
  time: ..."). Keep it under 120 characters. Without one, the card's first
  sentence is used.
- Headings inside fenced code blocks are ignored, so you can show Markdown examples.

## Checking your work

```sh
go run ./cmd/meanwhile content check                 # the built-in content
go run ./cmd/meanwhile content check --dir drafts/   # a folder of drafts
```

**Errors** fail CI:
- a missing or invalid field
- an ID that doesn't match the file name
- duplicate IDs
- a prerequisite that doesn't exist
- a prerequisite cycle (the path is printed, for example `a → b → a`)
- the wrong number of cards
- an empty card

**Warnings** are advice:
- card length
- recap length
- a concept with no prerequisites that nothing builds on
