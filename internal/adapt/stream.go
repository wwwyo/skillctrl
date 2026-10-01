package adapt

import "bytes"

type cappedOutput struct {
	// Embedding bytes.Buffer would promote ReadFrom and let io.Copy bypass the cap.
	buffer    bytes.Buffer
	remaining int
	exceeded  bool
}

func (output *cappedOutput) Write(data []byte) (int, error) {
	if len(data) > output.remaining {
		output.exceeded = true
		written, _ := output.buffer.Write(data[:output.remaining])
		output.remaining = 0
		return written, errTooLarge
	}
	written, err := output.buffer.Write(data)
	output.remaining -= written
	return written, err
}

type secretScanner struct {
	key, tail []byte
	found     bool
}

func (scanner *secretScanner) Write(data []byte) (int, error) {
	if scanner.found || len(scanner.key) == 0 {
		return len(data), nil
	}
	combined := append(scanner.tail, data...)
	if bytes.Contains(combined, scanner.key) {
		scanner.found = true
		scanner.tail = nil
		return len(data), nil
	}
	retained := min(len(combined), len(scanner.key)-1)
	// Keep the boundary bytes in their own allocation. Retaining a subslice of
	// the last chunk would otherwise retain its entire backing buffer.
	scanner.tail = append([]byte(nil), combined[len(combined)-retained:]...)
	return len(data), nil
}
