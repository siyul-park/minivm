# Lower Exceptions

Lowers source-language exception and generator constructs (`try`/`except`, `finally`, generator `throw` and `close`) onto minivm opcodes, for front-end authors.

`instruction-set.md` owns the semantics this guide relies on: Structured Errors for handlers and catch values, Coroutines for `YIELD`/`RESUME`. Every construct below uses existing opcodes; none needs VM support beyond them.

## Catch

A handler covers `[Start, End)` and catches every exception raised there. Lower a typed `except` clause to a filter at the catch IP:

```text
catch:                       ; stack: exc
  dup
  ref.test <error type>
  br_if is_error
  <match a non-Error value, or>  throw   ; rethrow
is_error:
  dup
  error.code
  i32.const K
  i32.eq
  br_if matched
  throw                      ; rethrow: keeps the frames of the first raise
matched:
  <clause body>
```

- Test the kind before `error.code`: a thrown value need not be an `Error`, and `error.code` traps on anything else.
- `Depth` counts the frame's slots (parameters and locals) plus the operands live at `Start`.

## Finally

Emit the `finally` body twice: on the normal exit path, and in a catch-all handler that runs it and rethrows.

```text
start:
  <try body>
end:
  <finally body>
  br after
catch:                       ; stack: exc
  <finally body>
  throw
after:
```

A `return` or `break` that leaves the `try` body emits the `finally` body before its jump.

## Generators

A generator is a coroutine (`instruction-set.md`, Coroutines). Two front-end conventions make it behave as Python and JavaScript generators do:

- **Lazy start.** Calling a coroutine-function runs its body to the first `YIELD`. Emit a prologue `YIELD` before any source code, outside every protected range, so creating the generator runs nothing.
- **Pending-throw slot.** The pending-throw slot is a global `P` that holds the exception a resumer is throwing into a suspended generator, or null. Declare it with the front end's exception root type or `error`. It `MUST NOT` be declared `any`: `ref.is_null` reads a zero scalar as null.

After every `YIELD`, the prologue's included, emit the check:

```text
  yield
check:
  global.get P
  ref.is_null
  br_if resume               ; nothing pending
  global.get P
  ref.null
  global.set P               ; clear before raising
  throw                      ; raised at the yield
resume:                      ; stack: the sent value
```

A `try` that encloses a source `yield` `MUST` cover the whole check, through its `throw`: threaded code searches handlers at the `throw`, not at the `YIELD`. The fused `global.get`/`ref.is_null`/`br_if` makes the check one dispatch per resume.

## Throw

Write `throw(g, exc)` once, as a front-end runtime function, not inline at each call site. `exc` `MUST` be a reference; wrap a scalar with `error.new`.

```text
throw(g, exc):
  local.get g
  coro.done
  br_if raise                ; finished: raise in the caller
start:
  local.get exc
  global.set P
  local.get g
  ref.null
  resume
  drop
end:
  br next
catch:                       ; RESUME trapped, or the generator did not catch exc
  ref.null
  global.set P               ; P is still set if the generator never ran
  throw
next:
  local.get g
  coro.done
  br_if stop                 ; finished: raise the source's StopIteration
  local.get g
  coro.value
  return                     ; the next yielded value
raise:
  local.get exc
  throw
```

The protected range is `[start, end)`. An exception the generator does not catch finishes it and reaches this handler; one it catches leaves it running to its next `YIELD`.

## Close

`close(g)` throws a reserved exit exception into the generator and accepts only its finishing:

1. If `coro.done` is true, return.
2. Call `throw(g, error.new(EXIT))`, where `EXIT` is a code the front end reserves, inside a handler whose filter (Catch) returns on `EXIT`, or on the StopIteration `throw` raises when the generator finishes, and rethrows anything else.
3. If `throw` returns, the generator yielded again: raise the source's runtime error.

A `finally` or `with` around a `yield` lowers as Finally; `close` runs it through the pending-throw slot. A generator that calls `close` or `throw` on itself traps `ErrCoroutineRunning`.

## Related

- `instruction-set.md` — Structured Errors and Coroutines rules
- `jit-internals.md` — how protected ranges and coroutines reach native code
- `fusion.md` — the fused slot-read, null-test, and branch sequence
