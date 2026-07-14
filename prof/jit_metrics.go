package prof

import (
	"sort"
	"strconv"
)

type jitMetrics struct {
	captures map[captureKey]*Counter
	compiles map[compileKey]*Counter
	emits    map[entryKey]*emitCounts
	entries  map[entryKey]*Counter
	yields   map[entryKey]*Counter
	exits    map[exitKey]*Counter
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

type emitCounts struct {
	emits uint64
	bytes uint64
}

func (m *jitMetrics) appendMetrics(out []Metric, collector *Collector) []Metric {
	captures := activeKeys(m.captures)
	sort.Slice(captures, func(i, j int) bool { return captures[i].less(captures[j]) })
	for _, key := range captures {
		out = append(out, Metric{
			Name: "vm_jit_trace_captures_total",
			Labels: []Label{
				{Key: "func", Value: strconv.Itoa(key.fn)},
				{Key: "ip", Value: strconv.Itoa(key.ip)},
				{Key: "outcome", Value: key.outcome.label()},
				{Key: "reason", Value: key.reason.label()},
			},
			Value: float64(m.captures[key].value),
		})
	}

	compiles := activeKeys(m.compiles)
	sort.Slice(compiles, func(i, j int) bool { return compiles[i].less(compiles[j]) })
	for _, key := range compiles {
		out = append(out, Metric{
			Name: "vm_jit_compiles_total",
			Labels: []Label{
				{Key: "func", Value: strconv.Itoa(key.fn)},
				{Key: "ip", Value: strconv.Itoa(key.ip)},
				{Key: "trigger", Value: key.trigger.label()},
				{Key: "frontend", Value: key.frontend.label()},
				{Key: "outcome", Value: key.outcome.label()},
				{Key: "reason", Value: key.reason.label()},
			},
			Value: float64(m.compiles[key].value),
		})
	}

	emits := make([]entryKey, 0, len(m.emits))
	for key, counts := range m.emits {
		if counts.emits > 0 || counts.bytes > 0 {
			emits = append(emits, key)
		}
	}
	sort.Slice(emits, func(i, j int) bool { return emits[i].less(emits[j]) })
	for _, key := range emits {
		labels := key.labels()
		counters := m.emits[key]
		if counters.emits > 0 {
			out = append(out, Metric{Name: "vm_jit_entry_emits_total", Labels: labels, Value: float64(counters.emits)})
		}
		if counters.bytes > 0 {
			out = append(out, Metric{Name: "vm_jit_entry_bytes_total", Labels: labels, Value: float64(counters.bytes)})
		}
	}

	out = appendEntryCounters(out, "vm_jit_native_entries_total", m.entries)
	out = appendEntryCounters(out, "vm_jit_native_yields_total", m.yields)

	exits := activeKeys(m.exits)
	sort.Slice(exits, func(i, j int) bool { return exits[i].less(exits[j]) })
	for _, key := range exits {
		opcode := "none"
		if key.opcode >= 0 && key.opcode <= 255 {
			opcode = collector.opcodeLabel(byte(key.opcode))
		}
		out = append(out, Metric{
			Name: "vm_jit_native_exits_total",
			Labels: []Label{
				{Key: "func", Value: strconv.Itoa(key.fn)},
				{Key: "ip", Value: strconv.Itoa(key.ip)},
				{Key: "kind", Value: key.kind.label()},
				{Key: "frontend", Value: key.frontend.label()},
				{Key: "reason", Value: key.reason.label()},
				{Key: "opcode", Value: opcode},
			},
			Value: float64(m.exits[key].value),
		})
	}
	return out
}

func (m *jitMetrics) merge(other *jitMetrics) {
	mergeCounters(&m.captures, other.captures)
	mergeCounters(&m.compiles, other.compiles)
	if len(other.emits) > 0 && m.emits == nil {
		m.emits = make(map[entryKey]*emitCounts)
	}
	for key, source := range other.emits {
		if source.emits == 0 && source.bytes == 0 {
			continue
		}
		destination := m.emits[key]
		if destination == nil {
			destination = &emitCounts{}
			m.emits[key] = destination
		}
		destination.emits += source.emits
		destination.bytes += source.bytes
	}
	mergeCounters(&m.entries, other.entries)
	mergeCounters(&m.yields, other.yields)
	mergeCounters(&m.exits, other.exits)
}

func (m *jitMetrics) reset() {
	resetCounters(m.captures)
	resetCounters(m.compiles)
	for _, counters := range m.emits {
		counters.emits = 0
		counters.bytes = 0
	}
	resetCounters(m.entries)
	resetCounters(m.yields)
	resetCounters(m.exits)
}

func (k captureKey) less(other captureKey) bool {
	return k.fn < other.fn ||
		k.fn == other.fn && (k.ip < other.ip ||
			k.ip == other.ip && (k.outcome < other.outcome ||
				k.outcome == other.outcome && k.reason < other.reason))
}

func (k compileKey) less(other compileKey) bool {
	return k.fn < other.fn ||
		k.fn == other.fn && (k.ip < other.ip ||
			k.ip == other.ip && (k.trigger < other.trigger ||
				k.trigger == other.trigger && (k.frontend < other.frontend ||
					k.frontend == other.frontend && (k.outcome < other.outcome ||
						k.outcome == other.outcome && k.reason < other.reason))))
}

func (k entryKey) labels() []Label {
	return []Label{
		{Key: "func", Value: strconv.Itoa(k.fn)},
		{Key: "ip", Value: strconv.Itoa(k.ip)},
		{Key: "kind", Value: k.kind.label()},
		{Key: "frontend", Value: k.frontend.label()},
	}
}

func (k entryKey) less(other entryKey) bool {
	return k.fn < other.fn ||
		k.fn == other.fn && (k.ip < other.ip ||
			k.ip == other.ip && (k.kind < other.kind ||
				k.kind == other.kind && k.frontend < other.frontend))
}

func (k exitKey) less(other exitKey) bool {
	return k.entryKey.less(other.entryKey) ||
		k.entryKey == other.entryKey && (k.reason < other.reason ||
			k.reason == other.reason && k.opcode < other.opcode)
}

func (v Frontend) label() string {
	switch v {
	case FrontendStatic:
		return "static"
	case FrontendTrace:
		return "trace"
	default:
		return "none"
	}
}

func (v Trigger) label() string {
	switch v {
	case TriggerHot:
		return "hot"
	case TriggerSideExit:
		return "side-exit"
	default:
		return "none"
	}
}

func (v EntryKind) label() string {
	switch v {
	case EntryStart:
		return "start"
	case EntryCall:
		return "call"
	case EntryLoop:
		return "loop"
	default:
		return "none"
	}
}

func (v CaptureOutcome) label() string {
	switch v {
	case CaptureOutcomePublished:
		return "published"
	case CaptureOutcomePartial:
		return "partial"
	case CaptureOutcomeRejected:
		return "rejected"
	default:
		return "none"
	}
}

func (v CaptureReason) label() string {
	switch v {
	case CaptureReasonAttemptLimit:
		return "attempt-limit"
	case CaptureReasonInvalidAnchor:
		return "invalid-anchor"
	case CaptureReasonHostCall:
		return "host-call"
	case CaptureReasonTailClosure:
		return "tail-closure"
	case CaptureReasonUnsupportedOp:
		return "unsupported-op"
	case CaptureReasonNestedTerminal:
		return "nested-terminal"
	case CaptureReasonStepTrap:
		return "step-trap"
	case CaptureReasonOpLimit:
		return "op-limit"
	default:
		return "none"
	}
}

func (v CompileOutcome) label() string {
	switch v {
	case CompileOutcomeEmitted:
		return "emitted"
	case CompileOutcomeEmpty:
		return "empty"
	case CompileOutcomeRejected:
		return "rejected"
	case CompileOutcomeError:
		return "error"
	default:
		return "none"
	}
}

func (v CompileReason) label() string {
	switch v {
	case CompileReasonNoInput:
		return "no-input"
	case CompileReasonNoPlan:
		return "no-plan"
	case CompileReasonInvalidPlan:
		return "invalid-plan"
	case CompileReasonLoweringRejected:
		return "lowering-rejected"
	case CompileReasonRegisterPressure:
		return "register-pressure"
	case CompileReasonBranchRange:
		return "branch-range"
	case CompileReasonBackendUnavailable:
		return "backend-unavailable"
	case CompileReasonError:
		return "error"
	default:
		return "none"
	}
}

func (v ExitReason) label() string {
	switch v {
	case ExitGuardKind:
		return "guard-kind"
	case ExitGuardShape:
		return "guard-shape"
	case ExitGuardBounds:
		return "guard-bounds"
	case ExitGuardValue:
		return "guard-value"
	case ExitColdBranch:
		return "cold-branch"
	case ExitTraceCut:
		return "trace-cut"
	case ExitTerminalOp:
		return "terminal-op"
	case ExitLoop:
		return "loop-exit"
	default:
		return "none"
	}
}

func appendEntryCounters(out []Metric, name string, rows map[entryKey]*Counter) []Metric {
	keys := activeKeys(rows)
	sort.Slice(keys, func(i, j int) bool { return keys[i].less(keys[j]) })
	for _, key := range keys {
		out = append(out, Metric{Name: name, Labels: key.labels(), Value: float64(rows[key].value)})
	}
	return out
}

func activeKeys[K comparable](rows map[K]*Counter) []K {
	keys := make([]K, 0, len(rows))
	for key, counter := range rows {
		if counter.value > 0 {
			keys = append(keys, key)
		}
	}
	return keys
}

func mergeCounters[K comparable](destination *map[K]*Counter, source map[K]*Counter) {
	for key, counter := range source {
		if counter.value > 0 {
			counterFor(destination, key).value += counter.value
		}
	}
}

func counterFor[K comparable](rows *map[K]*Counter, key K) *Counter {
	if *rows == nil {
		*rows = make(map[K]*Counter)
	}
	counter := (*rows)[key]
	if counter == nil {
		counter = &Counter{}
		(*rows)[key] = counter
	}
	return counter
}

func resetCounters[K comparable](rows map[K]*Counter) {
	for _, counter := range rows {
		counter.value = 0
	}
}
