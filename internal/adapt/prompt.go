package adapt

import _ "embed"

// Prompt is the review contract handed to the reviewing agent. It is embedded
// in the binary so a CI job that pins a released binary also pins the contract,
// with no separate file to fetch from a possibly untrusted checkout.
//
//go:embed ci-prompt.md
var Prompt string
