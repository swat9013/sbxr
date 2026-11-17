package runtime

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// SbxVersion は sbx の版 (v<major>.<minor>.<patch>)。
// 版の範囲は sbx 固有の約束なので、Runtime port には載せない (system.md「port の切り方」)。
type SbxVersion struct {
	Major, Minor, Patch int
}

func (v SbxVersion) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare は v が other より旧ければ負、同じなら 0、新しければ正を返す。
func (v SbxVersion) Compare(other SbxVersion) int {
	return cmp.Or(cmp.Compare(v.Major, other.Major), cmp.Compare(v.Minor, other.Minor), cmp.Compare(v.Patch, other.Patch))
}

var (
	// MinSbxVersion は sbxr が対応する sbx の下限。埋め込み kit が Kit spec v3 で、v0.51.0 より旧い sbx は v3 の kit を読めない (decision/0018)。
	MinSbxVersion = SbxVersion{Major: 0, Minor: 51, Patch: 0}
	// MeasuredSbxVersion は実測済みの上限。実 sbx で create から destroy までの 1 周を通した版へ上げる (decision/0019)。
	MeasuredSbxVersion = SbxVersion{Major: 0, Minor: 53, Patch: 0}
)

// Version は sbx version の出力 ("sbx version v0.53.0 <revision>"。sbx v0.53.0 の実測) から sbx の版を読む。
// 形の違う出力 (prerelease の版を含む) は、旧い版とも新しい版とも決められないので error にする。
func (s *Sbx) Version(ctx context.Context) (SbxVersion, error) {
	out, err := s.run(ctx, nil, "version")
	if err != nil {
		return SbxVersion{}, err
	}
	// 版の行の前後に sbx が別の行 (更新の通知など) を出しても読めるよう、"sbx version <版>" と読める最初の行を使う
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "sbx" || fields[1] != "version" {
			continue
		}
		if version, err := parseSbxVersion(fields[2]); err == nil {
			return version, nil
		}
	}
	return SbxVersion{}, fmt.Errorf("sbx version の出力から版を読めない (sbxr は sbx v0.53.0 で実測した形 \"sbx version v<major>.<minor>.<patch> <revision>\" の行を読む): %q", sbxOutputHead(out))
}

// parseSbxVersion は v<major>.<minor>.<patch> を読む。各欄は数字だけで、先頭の 0 を持たない。
func parseSbxVersion(text string) (SbxVersion, error) {
	malformed := fmt.Errorf("版 %q が v<major>.<minor>.<patch> の形でない", text)
	digits, found := strings.CutPrefix(text, "v")
	parts := strings.Split(digits, ".")
	if !found || len(parts) != 3 {
		return SbxVersion{}, malformed
	}
	var numbers [3]int
	for i, part := range parts {
		// Atoi は符号 (+1・-0) を受けるので、先に数字だけの欄かを確かめる
		if part == "" || strings.Trim(part, "0123456789") != "" || (len(part) > 1 && part[0] == '0') {
			return SbxVersion{}, malformed
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return SbxVersion{}, malformed
		}
		numbers[i] = n
	}
	return SbxVersion{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, nil
}
