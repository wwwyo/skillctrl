package adapt

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func TestPatchOutputRefusesTheFirstByteBeyondItsLimit(t *testing.T) {
	output := cappedOutput{remaining: 4}
	if n, err := output.Write([]byte("four")); n != 4 || err != nil {
		t.Fatalf("exact limit was refused: %d %v", n, err)
	}
	if n, err := output.Write([]byte("extra")); n != 0 || !errors.Is(err, errTooLarge) || !output.exceeded {
		t.Fatalf("oversized output was accepted: %d %v", n, err)
	}
	if output.buffer.String() != "four" {
		t.Fatal("oversized output was retained")
	}
	copied := cappedOutput{remaining: 4}
	if n, err := io.Copy(&copied, io.LimitReader(strings.NewReader("oversized"), 100)); n != 4 || !errors.Is(err, errTooLarge) {
		t.Fatalf("stream copy bypassed the size limit: %d %v", n, err)
	}
}

func TestCredentialScreeningCoversEveryStreamBoundary(t *testing.T) {
	key := "fixture-secret"
	for split := 0; split <= len(key); split++ {
		scanner := secretScanner{key: []byte(key)}
		for _, chunk := range []string{"before " + key[:split], key[split:] + " after"} {
			if n, err := scanner.Write([]byte(chunk)); n != len(chunk) || err != nil {
				t.Fatalf("scanner failed: %d %v", n, err)
			}
		}
		if !scanner.found {
			t.Fatalf("credential split at %d was missed", split)
		}
	}
	scanner := secretScanner{key: []byte(key)}
	for range 100 {
		_, _ = scanner.Write([]byte(strings.Repeat("safe", 1000)))
	}
	if scanner.found || len(scanner.tail) >= len(key) {
		t.Fatal("safe output was flagged or retained beyond the credential boundary")
	}
}
