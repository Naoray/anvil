package checks

import (
	"os"
)

// Assessment says whether a stored result still describes the checkout.
type Assessment struct {
	// Current is true when the result's identity equals the present one.
	Current bool `json:"current"`
	// PassingAndCurrent is the only state that may be treated as a pass.
	PassingAndCurrent bool `json:"passing_and_current"`
	// Reasons lists every identity component that changed.
	Reasons []string `json:"stale_reasons"`
}

// Assess compares a stored result with the current identity.
func Assess(result *Result, current Identity) Assessment {
	assessment := Assessment{Reasons: []string{}}
	if result == nil {
		assessment.Reasons = append(assessment.Reasons, "never run")
		return assessment
	}
	recorded := result.Identity
	add := func(changed bool, reason string) {
		if changed {
			assessment.Reasons = append(assessment.Reasons, reason)
		}
	}
	add(recorded.Git.Head != current.Git.Head, "commit changed")
	add(recorded.Git.Head == current.Git.Head && recorded.Git.Digest != current.Git.Digest, "uncommitted changes differ")
	add(recorded.DefinitionDigest != current.DefinitionDigest, "check definition changed")
	add(recorded.ToolchainDigest != current.ToolchainDigest, "toolchain changed")
	add(recorded.EnvDigest != current.EnvDigest, "environment changed")
	add(recorded.InputsDigest != current.InputsDigest, "inputs changed")
	add(recorded.Platform != current.Platform, "platform changed")
	assessment.Current = len(assessment.Reasons) == 0 && recorded.Digest == current.Digest
	if !assessment.Current && len(assessment.Reasons) == 0 {
		assessment.Reasons = append(assessment.Reasons, "identity changed")
	}
	assessment.PassingAndCurrent = assessment.Current && result.Status == StatusPassed
	return assessment
}

// ArtifactsIntact reports whether every recorded artifact still has its
// recorded digest; reuse is refused otherwise.
func ArtifactsIntact(result *Result) (bool, string) {
	if len(result.MissingArtifacts) > 0 {
		return false, "declared artifacts were missing when recorded"
	}
	for _, artifact := range result.Artifacts {
		if _, err := os.Stat(artifact.Path); err != nil {
			return false, "artifact " + artifact.Path + " is missing"
		}
		sum, _, err := fileDigest(artifact.Path)
		if err != nil || sum != artifact.SHA256 {
			return false, "artifact " + artifact.Path + " changed"
		}
	}
	return true, ""
}
