package runtime

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Version は実行基盤の版 (v<major>.<minor>.<patch>)。
type Version struct {
	Major, Minor, Patch int
}

func (v Version) String() string {
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// Compare は v が other より旧ければ負、同じなら 0、新しければ正を返す。
func (v Version) Compare(other Version) int {
	return cmp.Or(cmp.Compare(v.Major, other.Major), cmp.Compare(v.Minor, other.Minor), cmp.Compare(v.Patch, other.Patch))
}

var (
	// MinSbxVersion は sbxr が対応する sbx の下限。埋め込み kit が Kit spec v3 で、v0.51.0 より旧い sbx は v3 の kit を読めない (decision/0018)。
	MinSbxVersion = Version{Major: 0, Minor: 51, Patch: 0}
	// MeasuredSbxVersion は実測済みの上限。実 sbx で create から destroy までの 1 周を通した版へ上げる (decision/0019)。
	MeasuredSbxVersion = Version{Major: 0, Minor: 53, Patch: 0}
)

// Version は sbx version の出力 ("sbx version v0.53.0 <revision>"。sbx v0.53.0 の実測) から sbx の版を読む。
// 形の違う出力 (prerelease の版を含む) は、旧い版とも新しい版とも決められないので error にする。
func (s *Sbx) Version(ctx context.Context) (Version, error) {
	out, err := s.run(ctx, nil, "version")
	if err != nil {
		return Version{}, err
	}
	// 版の行の前に sbx が別の行 (更新の通知など) を出しても読めるよう、"sbx version " で始まる行を探す
	var fields []string
	for line := range strings.Lines(string(out)) {
		if strings.HasPrefix(line, "sbx version ") {
			fields = strings.Fields(line)
			break
		}
	}
	if len(fields) != 4 {
		return Version{}, fmt.Errorf("sbx version の出力から版を読めない (sbxr は sbx v0.53.0 で実測した形 \"sbx version v<major>.<minor>.<patch> <revision>\" の行を読む): %q", sbxOutputHead(out))
	}
	version, err := parseVersion(fields[2])
	if err != nil {
		return Version{}, fmt.Errorf("sbx version の出力から版を読めない: %w", err)
	}
	return version, nil
}

// parseVersion は v<major>.<minor>.<patch> を読む。
func parseVersion(text string) (Version, error) {
	parts := strings.Split(strings.TrimPrefix(text, "v"), ".")
	if !strings.HasPrefix(text, "v") || len(parts) != 3 {
		return Version{}, fmt.Errorf("版 %q が v<major>.<minor>.<patch> の形でない", text)
	}
	var numbers [3]int
	for i, part := range parts {
		// Atoi は符号 (+1・-0) を受けるので、数字だけの欄に限る
		n, err := strconv.Atoi(part)
		if err != nil || part == "" || strings.Trim(part, "0123456789") != "" || (len(part) > 1 && part[0] == '0') {
			return Version{}, fmt.Errorf("版 %q が v<major>.<minor>.<patch> の形でない", text)
		}
		numbers[i] = n
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, nil
}
