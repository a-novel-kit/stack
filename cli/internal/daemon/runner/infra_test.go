package runner

import (
	"maps"
	"sync"
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestInfraSession pins that a session copy shares no state with the live
// session. ensureOneShotSatisfied reads the copy's results without sessMu while
// StartInfra records results under it. A shared map turns that into a
// concurrent map read and write, which ends the daemon.
func TestInfraSession(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		// results is the live session's one-shot record. A nil map records no
		// session.
		results map[string]anovelv1.ExitReason
		wantOK  bool
	}{
		{name: "Success/Empty", results: map[string]anovelv1.ExitReason{}, wantOK: true},
		{
			name:    "Success/Recorded",
			results: map[string]anovelv1.ExitReason{"migrations": anovelv1.ExitReason_EXIT_REASON_SUCCESS},
			wantOK:  true,
		},
		{name: "Error/NotFound"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			key := sessionKey("default", "service-x")
			r := &Runner{infraSessions: map[string]*infraSession{}}
			if testCase.results != nil {
				sess := newInfraSession("default", "service-x")
				sess.Up = true
				maps.Copy(sess.OneShotResults, testCase.results)
				r.infraSessions[key] = sess
			}

			snapshot, ok := r.InfraSession("default", "service-x")
			if ok != testCase.wantOK {
				t.Fatalf("InfraSession: got ok %v, want %v", ok, testCase.wantOK)
			}

			// Record a one-shot under sessMu, as StartInfra does, while this
			// goroutine reads the copy unlocked. The race detector flags a
			// shared map here.
			var wg sync.WaitGroup
			wg.Go(func() {
				r.sessMu.Lock()
				defer r.sessMu.Unlock()
				if live, ok := r.infraSessions[key]; ok {
					live.OneShotResults["seed"] = anovelv1.ExitReason_EXIT_REASON_SUCCESS
				}
			})
			_, seeded := snapshot.OneShotResults["seed"]
			wg.Wait()

			if seeded || !maps.Equal(snapshot.OneShotResults, testCase.results) {
				t.Errorf("copy results: got %v, want %v, unchanged by the live session", snapshot.OneShotResults, testCase.results)
			}
		})
	}
}
