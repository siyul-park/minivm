package textparse

import (
	"bufio"
	"fmt"
	"io"
)

// MaxLineBytes is the maximum supported text parser line size.
const MaxLineBytes = 1 << 20 // 1 MiB

// NewScanner returns a line scanner configured for minivm text parsers.
func NewScanner(r io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)
	return scanner
}

// LineError wraps scanner errors with the parser line-size policy when possible.
func LineError(line int, err error) error {
	if err == bufio.ErrTooLong {
		return fmt.Errorf("line %d exceeds maximum allowed size of %d bytes", line, MaxLineBytes)
	}
	return err
}
