package runtime_test

import (
	"testing"

	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/runtimetest"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

func TestSbxKeepsTheRuntimeContract(t *testing.T) {
	runtimetest.Contract(t, func(*testing.T) runtimetest.Harness {
		stub := &sbxstub.Stub{VM: &sbxstub.FakeVM{}}
		return runtimetest.Harness{
			Runtime:        runtime.NewSbx(stub.Run),
			SandboxSecrets: func(sandbox string) int { return stub.SandboxSecrets[sandbox] },
		}
	})
}
