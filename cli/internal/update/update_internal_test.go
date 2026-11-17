package update

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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

			require.Equal(t, testCase.want, shouldNotify(testCase.current, testCase.latest))
		})
	}
}

// TestReadCache pins that readCache reads back what writeCache wrote, and fails
// on a missing file.
func TestReadCache(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		// A nested path also exercises writeCache's MkdirAll.
		path := filepath.Join(t.TempDir(), "nested", cacheFile)
		want := cacheEntry{LastCheck: time.Now().Truncate(time.Second), Latest: "v1.2.3"}
		require.NoError(t, writeCache(path, want))

		got, err := readCache(path)
		require.NoError(t, err)
		require.Equal(t, want.Latest, got.Latest)
		require.WithinDuration(t, want.LastCheck, got.LastCheck, 0)
	})

	t.Run("Error/MissingFile", func(t *testing.T) {
		t.Parallel()

		_, err := readCache(filepath.Join(t.TempDir(), "absent.json"))
		require.Error(t, err)
	})
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
				require.NoError(t, writeCache(path, *testCase.cached))
			}
			fetches := 0
			fetch := func() string {
				fetches++
				return testCase.fetched
			}
			for call := range 2 {
				require.Equal(t, testCase.want, latestVersion(path, fetch), "call %d", call+1)
			}
			require.Equal(t, testCase.wantFetches, fetches, "fetches over two calls")
		})
	}
}
