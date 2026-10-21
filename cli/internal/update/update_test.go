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
		{name: "Success/NewerPatch", current: "v1.0.0", latest: "v1.0.1", want: true},
		{name: "Success/NewerMajor", current: "v1.0.0", latest: "v2.0.0", want: true},
		{name: "Success/UpToDate", current: "v1.0.0", latest: "v1.0.0", want: false},
		{name: "Success/LocalIsAhead", current: "v1.2.0", latest: "v1.1.0", want: false},
		{name: "Success/DevBuild", current: "a1b2c3d", latest: "v1.0.1", want: false},
		{name: "Success/DirtyBuild", current: "a1b2c3d-dirty", latest: "v1.0.1", want: false},
		{name: "Success/LatestEmpty", current: "v1.0.0", latest: "", want: false},
		{name: "Success/LatestNotAVersion", current: "v1.0.0", latest: "garbage", want: false},
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

// TestLatestVersion pins when the check fetches: once per checkInterval, a
// failed fetch included, and again for a stale or invalid cache.
func TestLatestVersion(t *testing.T) {
	t.Parallel()

	now := time.Now()
	testCases := []struct {
		name    string
		cached  *cacheEntry // nil leaves no cache file
		fetched string      // what each fetch returns; "" is a failed fetch
		want    string
		// wantFetches counts the fetches over two consecutive calls.
		wantFetches int
	}{
		{name: "Success/FetchOnce", fetched: "v1.2.0", want: "v1.2.0", wantFetches: 1},
		{
			name:   "Success/FreshCache",
			cached: &cacheEntry{LastCheck: now, Latest: "v1.1.0"}, fetched: "v1.2.0", want: "v1.1.0",
		},
		{
			name:    "Success/StaleCache",
			cached:  &cacheEntry{LastCheck: now.Add(-checkInterval - time.Minute), Latest: "v1.1.0"},
			fetched: "v1.2.0", want: "v1.2.0", wantFetches: 1,
		},
		{
			name:   "Success/InvalidCache",
			cached: &cacheEntry{LastCheck: now, Latest: "garbage"}, fetched: "v1.2.0", want: "v1.2.0", wantFetches: 1,
		},
		{name: "Error/FailedFetchCached", want: "", wantFetches: 1},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), cacheFile)
			if testCase.cached != nil {
				if err := writeCache(path, *testCase.cached); err != nil {
					t.Fatalf("seed cache: %v", err)
				}
			}
			fetches := 0
			fetch := func() string {
				fetches++
				return testCase.fetched
			}
			for call := range 2 {
				if got := latestVersion(path, fetch); got != testCase.want {
					t.Fatalf("call %d: latestVersion = %q, want %q", call+1, got, testCase.want)
				}
			}
			if fetches != testCase.wantFetches {
				t.Errorf("fetched %d times over two calls, want %d", fetches, testCase.wantFetches)
			}
		})
	}
}
