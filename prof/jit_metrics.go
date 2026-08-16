package prof

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/siyul-park/minivm/instr"
)

// jitMetrics holds one counter table per JIT metric family. Every table maps
// an immutable key describing the row's labels to a stable counter handle, so
// a registered handle stays valid across merges and resets.
type jitMetrics struct {
	captures map[captureKey]*Counter
	compiles map[compileKey]*Counter
	emits    map[entryKey]*Counter
	bytes    map[entryKey]*Counter
	entries  map[entryKey]*Counter
	yields   map[entryKey]*Counter
	exits    map[exitKey]*Counter
}

// key constrains the label set of one JIT metric row. A row key is a
// comparable value type so it can index a counter table directly, and it
// orders itself against its own type so emitted rows are stable.
type key[K any] interface {
	comparable
	labels() []Label
	compare(other K) int
}

type captureKey struct {
	fn      int
	ip      int
	outcome CaptureOutcome
	reason  CaptureReason
}

type compileKey struct {
	fn       int
	ip       int
	trigger  Trigger
	frontend Frontend
	outcome  CompileOutcome
	reason   CompileReason
}

type entryKey struct {
	fn       int
	ip       int
	kind     EntryKind
	frontend Frontend
}

type exitKey struct {
	entryKey
	reason ExitReason
	opcode int
}

var (
	frontendLabels = [...]string{
		FrontendStatic: "static",
		FrontendTrace:  "trace",
	}
	triggerLabels = [...]string{
		TriggerHot:      "hot",
		TriggerSideExit: "side-exit",
	}
	entryLabels = [...]string{
		EntryStart: "start",
		EntryCall:  "call",
		EntryLoop:  "loop",
	}
	captureOutcomeLabels = [...]string{
		CaptureOutcomePublished: "published",
		CaptureOutcomePartial:   "partial",
		CaptureOutcomeRejected:  "rejected",
	}
	captureReasonLabels = [...]string{
		CaptureReasonAttemptLimit:   "attempt-limit",
		CaptureReasonInvalidAnchor:  "invalid-anchor",
		CaptureReasonHostCall:       "host-call",
		CaptureReasonHostObject:     "host-object",
		CaptureReasonTailClosure:    "tail-closure",
		CaptureReasonUnsupportedOp:  "unsupported-op",
		CaptureReasonNestedTerminal: "nested-terminal",
		CaptureReasonStepTrap:       "step-trap",
		CaptureReasonOpLimit:        "op-limit",
	}
	compileOutcomeLabels = [...]string{
		CompileOutcomeEmitted:  "emitted",
		CompileOutcomeEmpty:    "empty",
		CompileOutcomeRejected: "rejected",
		CompileOutcomeError:    "error",
	}
	compileReasonLabels = [...]string{
		CompileReasonNoInput:            "no-input",
		CompileReasonNoPlan:             "no-plan",
		CompileReasonInvalidPlan:        "invalid-plan",
		CompileReasonLoweringRejected:   "lowering-rejected",
		CompileReasonRegisterPressure:   "register-pressure",
		CompileReasonBranchRange:        "branch-range",
		CompileReasonBackendUnavailable: "backend-unavailable",
		CompileReasonError:              "error",
	}
	exitLabels = [...]string{
		ExitGuardKind:   "guard-kind",
		ExitGuardShape:  "guard-shape",
		ExitGuardBounds: "guard-bounds",
		ExitGuardValue:  "guard-value",
		ExitColdBranch:  "cold-branch",
		ExitTraceCut:    "trace-cut",
		ExitTerminalOp:  "terminal-op",
		ExitLoop:        "loop-exit",
	}
)

// appendMetrics appends every non-zero row of every family, each family's
// rows sorted by their label set so output is stable across runs.
func (m *jitMetrics) appendMetrics(out []Metric) []Metric {
	out = appendRows(out, "vm_jit_trace_captures_total", m.captures)
	out = appendRows(out, "vm_jit_compiles_total", m.compiles)
	out = appendRows(out, "vm_jit_entry_emits_total", m.emits)
	out = appendRows(out, "vm_jit_entry_bytes_total", m.bytes)
	out = appendRows(out, "vm_jit_native_entries_total", m.entries)
	out = appendRows(out, "vm_jit_native_yields_total", m.yields)
	return appendRows(out, "vm_jit_native_exits_total", m.exits)
}

func (m *jitMetrics) merge(other *jitMetrics) {
	mergeRows(&m.captures, other.captures)
	mergeRows(&m.compiles, other.compiles)
	mergeRows(&m.emits, other.emits)
	mergeRows(&m.bytes, other.bytes)
	mergeRows(&m.entries, other.entries)
	mergeRows(&m.yields, other.yields)
	mergeRows(&m.exits, other.exits)
}

func (m *jitMetrics) reset() {
	resetRows(m.captures)
	resetRows(m.compiles)
	resetRows(m.emits)
	resetRows(m.bytes)
	resetRows(m.entries)
	resetRows(m.yields)
	resetRows(m.exits)
}

func (k captureKey) labels() []Label {
	return []Label{
		{Key: "func", Value: strconv.Itoa(k.fn)},
		{Key: "ip", Value: strconv.Itoa(k.ip)},
		{Key: "outcome", Value: label(captureOutcomeLabels[:], int(k.outcome))},
		{Key: "reason", Value: label(captureReasonLabels[:], int(k.reason))},
	}
}

func (k captureKey) compare(o captureKey) int {
	return cmp.Or(
		cmp.Compare(k.fn, o.fn),
		cmp.Compare(k.ip, o.ip),
		cmp.Compare(k.outcome, o.outcome),
		cmp.Compare(k.reason, o.reason),
	)
}

func (k compileKey) labels() []Label {
	return []Label{
		{Key: "func", Value: strconv.Itoa(k.fn)},
		{Key: "ip", Value: strconv.Itoa(k.ip)},
		{Key: "trigger", Value: label(triggerLabels[:], int(k.trigger))},
		{Key: "frontend", Value: label(frontendLabels[:], int(k.frontend))},
		{Key: "outcome", Value: label(compileOutcomeLabels[:], int(k.outcome))},
		{Key: "reason", Value: label(compileReasonLabels[:], int(k.reason))},
	}
}

func (k compileKey) compare(o compileKey) int {
	return cmp.Or(
		cmp.Compare(k.fn, o.fn),
		cmp.Compare(k.ip, o.ip),
		cmp.Compare(k.trigger, o.trigger),
		cmp.Compare(k.frontend, o.frontend),
		cmp.Compare(k.outcome, o.outcome),
		cmp.Compare(k.reason, o.reason),
	)
}

func (k entryKey) labels() []Label {
	return []Label{
		{Key: "func", Value: strconv.Itoa(k.fn)},
		{Key: "ip", Value: strconv.Itoa(k.ip)},
		{Key: "kind", Value: label(entryLabels[:], int(k.kind))},
		{Key: "frontend", Value: label(frontendLabels[:], int(k.frontend))},
	}
}

func (k entryKey) compare(other entryKey) int {
	return cmp.Or(
		cmp.Compare(k.fn, other.fn),
		cmp.Compare(k.ip, other.ip),
		cmp.Compare(k.kind, other.kind),
		cmp.Compare(k.frontend, other.frontend),
	)
}

func (k exitKey) labels() []Label {
	opcode := "none"
	if k.opcode >= 0 && k.opcode <= 255 {
		opcode = opcodeLabel(byte(k.opcode))
	}
	return append(k.entryKey.labels(),
		Label{Key: "reason", Value: label(exitLabels[:], int(k.reason))},
		Label{Key: "opcode", Value: opcode},
	)
}

func (k exitKey) compare(o exitKey) int {
	return cmp.Or(
		k.entryKey.compare(o.entryKey),
		cmp.Compare(k.reason, o.reason),
		cmp.Compare(k.opcode, o.opcode),
	)
}

// appendRows appends one metric per non-zero row, in label order.
func appendRows[K key[K]](out []Metric, name string, rows map[K]*Counter) []Metric {
	active := make([]K, 0, len(rows))
	for row, counter := range rows {
		if counter.value > 0 {
			active = append(active, row)
		}
	}
	slices.SortFunc(active, func(a, b K) int { return a.compare(b) })
	for _, row := range active {
		out = append(out, Metric{Name: name, Labels: row.labels(), Value: float64(rows[row].value)})
	}
	return out
}

func mergeRows[K comparable](rows *map[K]*Counter, source map[K]*Counter) {
	for row, counter := range source {
		if counter.value > 0 {
			register(rows, row).value += counter.value
		}
	}
}

func resetRows[K comparable](rows map[K]*Counter) {
	for _, counter := range rows {
		counter.value = 0
	}
}

// register returns row's counter, creating the table and the counter on first
// use. The handle is stable, so a caller may keep it and increment it directly.
func register[K comparable](rows *map[K]*Counter, row K) *Counter {
	if *rows == nil {
		*rows = make(map[K]*Counter)
	}
	counter := (*rows)[row]
	if counter == nil {
		counter = &Counter{}
		(*rows)[row] = counter
	}
	return counter
}

// label names an enum value, falling back to "none" for the zero value and
// for anything outside the declared range.
func label(names []string, value int) string {
	if value < 0 || value >= len(names) || names[value] == "" {
		return "none"
	}
	return names[value]
}

// opcodeLabel names an opcode by mnemonic, falling back to its hex code.
func opcodeLabel(code byte) string {
	if typ := instr.TypeOf(instr.Opcode(code)); typ.Mnemonic != "" {
		return typ.Mnemonic
	}
	return "0x" + strconv.FormatInt(int64(code), 16)
}
