// Package herdrtest は test 用の host の herdr を置く。herdr machine の module と、それを使う lifecycle の test で共有する。
package herdrtest

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/swat9013/sbxr/internal/herdr"
)

// Fake は host の herdr を再現する。呼ばれた操作を Calls に起きた順に並べる。
type Fake struct {
	Machines []herdr.Machine
	Calls    []string
	// Missing が true なら host に herdr が無い。
	Missing                          bool
	FailAdd, FailDisable, FailRemove bool
}

var _ herdr.Client = (*Fake)(nil)

func (f *Fake) Available() error {
	f.Calls = append(f.Calls, "available")
	if f.Missing {
		return errors.New("host に herdr が無い")
	}
	return nil
}

func (f *Fake) List(context.Context) ([]herdr.Machine, error) {
	f.Calls = append(f.Calls, "list")
	return slices.Clone(f.Machines), nil
}

func (f *Fake) Add(_ context.Context, target, label string) error {
	f.Calls = append(f.Calls, "add "+target+" "+label)
	if f.FailAdd {
		return errors.New("herdr machine add: exit status 1")
	}
	f.Machines = append(f.Machines, herdr.Machine{ID: fmt.Sprintf("id%d", len(f.Machines)+1), Target: target, Enabled: true})
	return nil
}

func (f *Fake) Enable(_ context.Context, id string) error {
	f.Calls = append(f.Calls, "enable "+id)
	f.setEnabled(id, true)
	return nil
}

func (f *Fake) Disable(_ context.Context, id string) error {
	f.Calls = append(f.Calls, "disable "+id)
	if f.FailDisable {
		return errors.New("herdr machine disable: exit status 1")
	}
	f.setEnabled(id, false)
	return nil
}

func (f *Fake) Remove(_ context.Context, id string) error {
	f.Calls = append(f.Calls, "remove "+id)
	if f.FailRemove {
		return errors.New("herdr machine remove: exit status 1")
	}
	f.Machines = slices.DeleteFunc(f.Machines, func(m herdr.Machine) bool { return m.ID == id })
	return nil
}

func (f *Fake) setEnabled(id string, enabled bool) {
	for i := range f.Machines {
		if f.Machines[i].ID == id {
			f.Machines[i].Enabled = enabled
		}
	}
}
