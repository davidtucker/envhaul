package version

import (
	"runtime"
	"testing"
)

func TestGetReleaseBuild(t *testing.T) {
	defer func(v, c, d string) { Version, Commit, Date = v, c, d }(Version, Commit, Date)
	Version, Commit, Date = "1.2.3", "abc123", "2026-10-26T00:00:00Z"

	got := Get()
	want := Info{
		Version: "1.2.3", Commit: "abc123", Date: "2026-10-26T00:00:00Z",
		GoVersion: runtime.Version(), Platform: runtime.GOOS + "/" + runtime.GOARCH,
	}
	if got != want {
		t.Errorf("Get() = %+v\nwant  %+v", got, want)
	}
}

func TestGetDevelopmentBuild(t *testing.T) {
	defer func(v, c, d string) { Version, Commit, Date = v, c, d }(Version, Commit, Date)
	Version, Commit, Date = "", "", ""

	// Without ldflags, values come from the embedded build info, falling
	// back to placeholders. Nothing may be empty.
	got := Get()
	if got.Version == "" || got.Commit == "" || got.Date == "" {
		t.Errorf("Get() = %+v, want no empty fields", got)
	}
}
