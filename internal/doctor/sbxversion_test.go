package doctor

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/swat9013/sbxr/internal/herdr/herdrtest"
	"github.com/swat9013/sbxr/internal/runtime"
	"github.com/swat9013/sbxr/internal/runtime/sbxstub"
)

// sbxVersionItem は sbx stub が sbxVersion を版として返すときの、項目「sbx の版」の結果。
func sbxVersionItem(t *testing.T, stub *sbxstub.Stub) Item {
	t.Helper()
	dir := t.TempDir()
	d := Doctor{
		Runtime:      runtime.NewSbx(stub.Run),
		SbxAvailable: func() error { return nil },
		Herdr:        &herdrtest.Fake{},
		UserConfig:   filepath.Join(dir, "config.yaml"),
		SecretFile:   filepath.Join(dir, "secrets.env"),
	}
	for _, section := range d.Diagnose(context.Background()).Sections {
		for _, item := range section.Items {
			if item.Name == "sbx の版" {
				return item
			}
		}
	}
	t.Fatalf("doctor report has no item %q", "sbx の版")
	return Item{}
}

func TestSbxVersionOlderThanTheSupportedMinimumFailsAndTellsToUpdateSbx(t *testing.T) {
	item := sbxVersionItem(t, &sbxstub.Stub{Version: "v0.50.2"})

	if item.Status != Fail {
		t.Errorf("status = %s, want fail for a version below %s", item.Status, runtime.MinSbxVersion)
	}
	if !strings.Contains(item.Fix, "sbx を "+runtime.MinSbxVersion.String()+" 以上へ更新する") {
		t.Errorf("fix = %q, want updating sbx shown", item.Fix)
	}
}

func TestSbxVersionNewerThanTheMeasuredMaximumStaysOKWithANote(t *testing.T) {
	newer := runtime.MeasuredSbxVersion
	newer.Minor++

	item := sbxVersionItem(t, &sbxstub.Stub{Version: newer.String()})

	if item.Status != OK {
		t.Errorf("status = %s, want ok for a version above the measured maximum", item.Status)
	}
	if !strings.Contains(item.Detail, outsideMeasuredNote) {
		t.Errorf("detail = %q, want the note that the version is outside the measured range", item.Detail)
	}
}

func TestSbxVersionThatCannotBeReadFails(t *testing.T) {
	for _, tt := range []struct {
		name string
		stub *sbxstub.Stub
	}{
		{name: "output without a version", stub: &sbxstub.Stub{Version: "dev"}},
		{name: "command fails", stub: &sbxstub.Stub{FailOn: "version"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			item := sbxVersionItem(t, tt.stub)

			if item.Status != Fail {
				t.Errorf("status = %s (%s), want fail when the version cannot be read", item.Status, item.Detail)
			}
			if !strings.Contains(item.Fix, "sbx を "+runtime.MinSbxVersion.String()+" 以上へ更新する") {
				t.Errorf("fix = %q, want updating sbx shown", item.Fix)
			}
		})
	}
}

func TestSbxVersionWithinTheMeasuredRangeIsOKWithoutANote(t *testing.T) {
	for _, version := range []runtime.Version{runtime.MinSbxVersion, runtime.MeasuredSbxVersion} {
		t.Run(version.String(), func(t *testing.T) {
			item := sbxVersionItem(t, &sbxstub.Stub{Version: version.String()})

			if item.Status != OK {
				t.Errorf("status = %s (%s), want ok", item.Status, item.Detail)
			}
			if strings.Contains(item.Detail, outsideMeasuredNote) {
				t.Errorf("detail = %q, want no note within the measured range", item.Detail)
			}
		})
	}
}

func TestSbxVersionIsSkippedWhenSbxIsMissing(t *testing.T) {
	dir := t.TempDir()
	d := Doctor{
		Runtime:      runtime.NewSbx((&sbxstub.Stub{FailOn: "version"}).Run),
		SbxAvailable: func() error { return errors.New("sbx が PATH に無い") },
		Herdr:        &herdrtest.Fake{},
		UserConfig:   filepath.Join(dir, "config.yaml"),
		SecretFile:   filepath.Join(dir, "secrets.env"),
	}

	item := d.sbxVersionItem(context.Background(), d.readHostAndUser())

	if item.Status != Skip {
		t.Errorf("status = %s (%s), want skip when sbx is missing", item.Status, item.Detail)
	}
}
