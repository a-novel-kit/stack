package update

import (
	"path/filepath"
	"testing"
	"time"
)

func TestShouldNotify(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		current string
		latest  string

		want bool
	}{
		{name: "newer patch available", current: "v1.0.0", latest: "v1.0.1", want: true},
		{name: "newer major available", current: "v1.0.0", latest: "v2.0.0", want: true},
		{name: "up to date", current: "v1.0.0", latest: "v1.0.0", want: false},
		{name: "local is ahead", current: "v1.2.0", latest: "v1.1.0", want: false},
		{name: "current is a dev build", current: "a1b2c3d", latest: "v1.0.1", want: false},
		{name: "current is a dirty build", current: "a1b2c3d-dirty", latest: "v1.0.1", want: false},
		{name: "latest empty (fetch failed)", current: "v1.0.0", latest: "", want: false},
		{name: "latest not a version", current: "v1.0.0", latest: "garbage", want: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := shouldNotify(testCase.current, testCase.latest); got != testCase.want {
				t.Errorf("shouldNotify(%q, %q) = %v, want %v",
					testCase.current, testCase.latest, got, testCase.want)
			}
		})
	}
}

func TestCacheRoundTrip(t *testing.T) {
	t.Parallel()

	// A nested path also exercises writeCache's MkdirAll.
	path := filepath.Join(t.TempDir(), "nested", cacheFile)
	want := cacheEntry{LastCheck: time.Now().Truncate(time.Second), Latest: "v1.2.3"}

	if err := writeCache(path, want); err != nil {
		t.Fatalf("writeCache: %v", err)
	}
	got, err := readCache(path)
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if got.Latest != want.Latest || !got.LastCheck.Equal(want.LastCheck) {
		t.Errorf("roundtrip = %+v, want %+v", got, want)
	}
}

func TestReadCacheMissing(t *testing.T) {
	t.Parallel()

	if _, err := readCache(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Error("expected an error reading a missing cache file, got nil")
	}
}
