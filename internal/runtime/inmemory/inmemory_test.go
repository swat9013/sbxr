package inmemory

import (
	"testing"

	"github.com/swat9013/sbxr/internal/runtime/runtimetest"
)

func TestInMemoryKeepsTheRuntimeContract(t *testing.T) {
	runtimetest.Contract(t, func(*testing.T) runtimetest.Harness {
		rt := New()
		return runtimetest.Harness{
			Runtime:        rt,
			SandboxSecrets: func(sandbox string) int { return len(rt.Sandbox(sandbox).Secrets) },
			SandboxRules:   func(sandbox string) []string { return rt.Sandbox(sandbox).EgressRules },
		}
	})
}
