package instr

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Position maps the instructions from IP up to the next Position's IP (or the
// end of code) to the opaque source ID a front-end assigned; Source 0 is none.
type Position struct {
	IP     int
	Source uint32
}

// Find returns the Source of the Position covering ip, or 0 when none does.
// positions must be sorted by IP.
func Find(positions []Position, ip int) uint32 {
	i, found := slices.BinarySearchFunc(positions, ip, func(p Position, ip int) int {
		return p.IP - ip
	})
	if found {
		return positions[i].Source
	}
	if i == 0 {
		return 0
	}
	return positions[i-1].Source
}

// ParsePosition parses the "ip=N source=M" form Position.String writes.
func ParsePosition(s string) (Position, error) {
	var p Position
	var hasIP, hasSource bool
	for _, f := range strings.Fields(s) {
		key, val, ok := strings.Cut(f, "=")
		if !ok {
			return Position{}, fmt.Errorf("invalid position field %q (expected key=value)", f)
		}
		switch key {
		case "ip":
			v, err := strconv.ParseUint(val, 10, 31)
			if err != nil {
				return Position{}, fmt.Errorf("invalid position value %q: %w", val, err)
			}
			p.IP, hasIP = int(v), true
		case "source":
			v, err := strconv.ParseUint(val, 10, 32)
			if err != nil {
				return Position{}, fmt.Errorf("invalid position value %q: %w", val, err)
			}
			p.Source, hasSource = uint32(v), true
		default:
			return Position{}, fmt.Errorf("unknown position field %q", key)
		}
	}
	if !hasIP || !hasSource {
		return Position{}, fmt.Errorf("position %q needs ip and source", s)
	}
	return p, nil
}

func (p Position) String() string {
	return fmt.Sprintf("ip=%d source=%d", p.IP, p.Source)
}
