package main

import "runtime/debug"

// resolveVersion は表示する版を ldflags、build info (go install 時に記録される module の版) の順で決める。
// どちらも無ければ Go の慣例に合わせて "(devel)" を返す。
func resolveVersion(ldflags string, info *debug.BuildInfo) string {
	if ldflags != "" {
		return ldflags
	}
	if info != nil && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}
