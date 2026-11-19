package main

import (
	"testing"

	"github.com/swat9013/sbxr/internal/guide"
)

func TestGuidePrintsTheWholeGuideAndSucceeds(t *testing.T) {
	out, err := runSbxr(t, dependencies{}, "guide")

	if err != nil || out != guide.Text {
		t.Errorf("sbxr guide error = %v, output = %q, want the whole guide and success", err, out)
	}
}

func TestGuideTakesNoArguments(t *testing.T) {
	_, err := runSbxr(t, dependencies{}, "guide", "extra")

	if err == nil {
		t.Error("sbxr guide extra succeeded, want an argument rejected")
	}
}
