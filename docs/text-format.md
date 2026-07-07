# Program Text Format

Canonical, versioned assembly syntax for `program.Program.String()` and `program.Parse`.

## When to Read

Read this when changing `program.Program`, the parser/formatter pair, CLI assembly/disassembly behavior, golden fixtures, or tools that exchange minivm programs as text.

## Source of Truth

- Formatter: `program/program.go` `Program.String`.
- Parser: `program/parse.go` `Parse`.
- Instruction line syntax: `instr.Format` and `instr.ParseAll`.
- Type and function literal syntax: `types.Type.String`, `types.Parse`, and `types.ParseFunction`.

## Design Model

The text format is an assembler-style translation unit. It follows the same principles used by established textual IR and assembly formats:

| Reference | Adopted convention |
|---|---|
| WAT / WebAssembly text | module-level declarations and explicit tables |
| LLVM IR | stable textual IR that preserves typed semantic state |
| MLIR | text, memory, and serialized forms describe the same structure |
| GAS / NASM | line-oriented instructions, labels/offsets, and directives |

The format is intentionally directive-based. Semantic sections are identified by headers such as `.code` and `.handlers`; the parser does not infer section meaning from blank-line position or entry shape in canonical input.

## Canonical Shape

`Program.String()` emits sections in this order:

```asm
.version 1

.code
0000:	i32.const 0x00000001
0005:	local.set 0x00000000

.locals
0000:	i32
0001:	ref

.constants
0000:	i32 7
0001:	func(i32) i32
	0000:	local.get 0x00000000
	0002:	return

.types
0000:	func(i32) i32

.handlers
0000:	start=0000 end=0007 catch=0007 depth=1
```

`.version 1` is always emitted. `.code` is always emitted, even for an empty program. Metadata sections are omitted when empty.

## Sections

### `.version`

Declares the program text format version. Version `1` rejects unknown sections instead of skipping them, so new metadata cannot silently change program semantics.

### `.code`

Contains instruction lines in `instr.Format` syntax. Offsets are decimal byte positions padded to four digits, followed by a tab and the instruction body.

### `.locals`

Contains entry-frame local types, one indexed type per line. The table preserves `Program.Locals` exactly.

### `.constants`

Contains indexed constants. Function constants use the existing multi-line `types.Function.String()` body, with continuation lines indented by a tab. Primitive constants use typed literals:

```asm
0000:	i1 true
0001:	i8 -1
0002:	i32 42
0003:	i64 42
0004:	f32 1.5
0005:	f64 1.5
0006:	ref 0
0007:	string "hello"
```

### `.types`

Contains indexed type declarations in `types.Type.String()` syntax.

### `.handlers`

Contains the top-level exception table. Each handler is a single indexed line with explicit fields:

```asm
0000:	start=0000 end=0015 catch=0018 depth=0
```

`start`, `end`, and `catch` are byte offsets in the `.code` section. `depth` is the entry stack depth stored in `instr.Handler.Depth`.

## Parser Rules

- Canonical input is parsed by section header only.
- Duplicate sections are rejected.
- Unknown sections are rejected for version `1`.
- Indexed metadata entries must be dense and zero-based.
- Parse errors include the physical line number and section name when available.
- Pre-v1 unsectioned input is accepted only by the legacy compatibility path.

## Maintenance Notes

Keep this document synchronized with `Program.String()` and `program.Parse`. When adding a semantically relevant `Program` field, add a section or an explicit field to an existing section before relying on text round-trips in fixtures or tooling.

## Related Docs

- `instruction-set.md` for instruction semantics and operand widths.
- `verification.md` for static checks applied after loading bytecode.
- `value-representation.md` for boxed primitive values.
- `host-integration.md` for host-side function/value conversion.
