package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	_ "github.com/siyul-park/minivm/benchmarks/fixtures"
	"github.com/siyul-park/minivm/benchmarks/registry"
)

type sample struct {
	ns    float64
	bytes float64
	alloc float64
	mem   bool
}

var columns = []string{
	"Threaded",
	"JIT",
	"JIT speedup",
	"Wazero",
	"Native Go",
	"Tengo",
	"GopherLua",
	"Goja",
	"gpython",
	"CPython",
	"Yaegi",
}

func main() {
	input := flag.String("input", "", "benchmark output")
	output := flag.String("output", "", "markdown document")
	flag.Parse()
	if *input == "" || *output == "" {
		fail("input and output are required")
	}

	samples, err := parse(*input)
	if err != nil {
		fail("%v", err)
	}
	if err := write(*output, samples); err != nil {
		fail("%v", err)
	}
}

func parse(path string) (map[string]map[string]sample, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	out := make(map[string]map[string]sample)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "BenchmarkKernels/") {
			continue
		}
		parts := strings.Split(fields[0], "/")
		if len(parts) != 3 {
			continue
		}
		runtime := parts[2]
		if index := strings.LastIndexByte(runtime, '-'); index >= 0 {
			runtime = runtime[:index]
		}
		ns, err := metric(fields, "ns/op")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", fields[0], err)
		}
		value := sample{ns: ns}
		if bytes, ok := optionalMetric(fields, "B/op"); ok {
			value.bytes = bytes
			value.mem = true
		}
		if allocs, ok := optionalMetric(fields, "allocs/op"); ok {
			value.alloc = allocs
			value.mem = true
		}
		if out[parts[1]] == nil {
			out[parts[1]] = make(map[string]sample)
		}
		out[parts[1]][runtime] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	for _, spec := range registry.All() {
		if out[spec.Name] == nil {
			return nil, fmt.Errorf("missing benchmark output: %s", spec.Name)
		}
		for _, runtime := range []string{"threaded", "jit"} {
			if _, ok := out[spec.Name][runtime]; !ok {
				return nil, fmt.Errorf("missing %s/%s", spec.Name, runtime)
			}
		}
	}
	return out, nil
}

func write(path string, samples map[string]map[string]sample) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	doc := string(data)
	start := "<!-- benchmark-table:start -->"
	end := "<!-- benchmark-table:end -->"
	begin := strings.Index(doc, start)
	finish := strings.Index(doc, end)
	if begin < 0 || finish < begin {
		return fmt.Errorf("benchmark table markers not found")
	}
	table := render(samples)
	doc = doc[:begin] + start + "\n\n" + table + "\n\n" + end + doc[finish+len(end):]
	return os.WriteFile(path, []byte(doc), 0o644)
}

func render(samples map[string]map[string]sample) string {
	names := make([]string, 0, len(samples))
	for name := range samples {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("| Kernel | ")
	b.WriteString(strings.Join(columns, " | "))
	b.WriteString(" |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, name := range names {
		row := samples[name]
		values := []string{
			value(row, "threaded"),
			value(row, "jit"),
			speedup(row),
			value(row, "wazero"),
			value(row, "native"),
			value(row, "tengo"),
			value(row, "gopher_lua"),
			value(row, "goja"),
			value(row, "gpython"),
			value(row, "cpython"),
			value(row, "yaegi"),
		}
		b.WriteString("| " + name + " | " + strings.Join(values, " | ") + " |\n")
	}
	return b.String()
}

func value(row map[string]sample, runtime string) string {
	item, ok := row[runtime]
	if !ok {
		return "—"
	}
	return formatSample(item)
}

func speedup(row map[string]sample) string {
	threaded, ok := row["threaded"]
	if !ok {
		return "—"
	}
	jit, ok := row["jit"]
	if !ok || jit.ns == 0 {
		return "—"
	}
	return fmt.Sprintf("%.3g×", threaded.ns/jit.ns)
}

func formatSample(value sample) string {
	text := formatTime(value.ns)
	if value.mem {
		text += fmt.Sprintf(" · %.0fB · %.0fa", value.bytes, value.alloc)
	}
	return text
}

func formatTime(ns float64) string {
	switch {
	case ns < 1_000:
		return fmt.Sprintf("%.4g ns", ns)
	case ns < 1_000_000:
		return fmt.Sprintf("%.4g µs", ns/1_000)
	case ns < 1_000_000_000:
		return fmt.Sprintf("%.4g ms", ns/1_000_000)
	default:
		return fmt.Sprintf("%.4g s", ns/1_000_000_000)
	}
}

func metric(fields []string, name string) (float64, error) {
	value, ok := optionalMetric(fields, name)
	if !ok {
		return 0, fmt.Errorf("missing %s", name)
	}
	return value, nil
}

func optionalMetric(fields []string, name string) (float64, bool) {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i+1] != name {
			continue
		}
		value, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
