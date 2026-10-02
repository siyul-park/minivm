# Writing

Owns how every Markdown document in the repository and every plan, brief, and report an agent writes is written. Readers are contributors and agents who write or review documents. `README.md` owns which document holds which topic.

## Purpose

- A document `MUST` have one purpose and one intended reader.
- Its scope sentence, directly below H1, states both: what the document owns or enables, and for whom.
- The reader fixes the assumed knowledge and the level of detail. Content that does not serve the purpose `MUST` be removed or moved to its owner.

## Roles

| Role | Holds | History |
|---|---|---|
| Topic doc | current behavior and contracts of one topic | `MUST NOT` |
| Guide (`guides/`) | a procedure over topic contracts; contract text stays with its owner | `MUST NOT` |
| `roadmap.md` | priorities only | `MUST NOT` |
| History doc (`jit-lessons.md`) | dated lessons with evidence | `MAY` |
| Plan, audit, brief, report (`plans/`, `superpowers/`) | dated decisions, evidence, future work; never current authority | `MAY` |
| `AGENTS.md` | repository workflow | `MUST NOT` |

## Ownership

- Every fact `MUST` have exactly one owner document; two documents `MUST NOT` own the same fact.
- Any other document that needs the fact `MUST` link to its owner instead of restating it. A restatement found in review is replaced by a link.
- A changed fact is edited in its owner only.
- Code owns implementation detail. Documents state contracts and name symbols, never line numbers.

## Structure

- Split a document into H2 sections, one topic each. No two sections share a topic, and no section repeats another's point.
- For each section, decide what the reader needs to reach the document's purpose, then write only that.
- Order by need: the key fact first, then detail, then rationale. The same holds within a section and within a sentence.
- A heading names its topic in one or two words.

## Form

Each kind of information `MUST` use the form that delivers it best:

| Information | Form |
|---|---|
| Comparison, per-case rule, mapping | table |
| Set of independent items | bulleted list |
| Ordered steps | numbered list |
| Command, code, output | fenced code block with its language |
| Structure or flow | `text` code block diagram |
| Rationale | short prose |

## Terminology

- Use the established term of the field: Go, the language and platform specifications, compiler and VM literature, RFCs. A new term `MUST NOT` be coined where an established one exists.
- A project term is allowed only for a concept with no standard name; its owner defines it once and every document uses it unchanged.
- One term per concept and one concept per term: no synonyms for variety.
- Requirements use RFC 2119 keywords (`MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, `MAY`) in code font. A bare imperative is not a requirement; normative wording belongs in the owner.
- Identifiers, paths, and commands are written in code font exactly as in the source.

## Currency

- Topic docs, guides, and `AGENTS.md` state current facts only. They `MUST NOT` accumulate history: no changelogs, dates, removed behavior, or "previously", "now", and "new" wording.
- When behavior changes, rewrite the fact in place; never append a correction.
- History lives in git and in the history, plan, and audit roles above.

## Concision

- Every sentence `MUST` carry information the reader needs. Delete filler, hedging, restated headings, and summaries of the body.
- Prefer short sentences, active voice, and present tense.
- Be specific: numbers with units, named symbols, exact conditions.

## Format

Every document `MUST` be valid GitHub-flavored Markdown. Every topic doc and guide `MUST`:

- use H1 for the document subject and unnumbered H2 headings;
- place the scope sentence directly below H1;
- use an `Ownership` section for canonical owner maps where ownership applies;
- end with a `Related` section;
- label every fenced code block with its language;
- keep one blank line between prose, lists, tables, and code blocks.

## Related

- `README.md` — topic-to-document map
- `AGENTS.md` — repository workflow
- `coding-patterns.md` — code comments and naming
