//go:build !linux

package metrics

import "time"

const processMetricsAvailable = false

// readProcess is unsupported outside Linux: only players and server states
// are recorded there.
func readProcess(int) (time.Duration, int64, bool) { return 0, 0, false }

// readContainer is unsupported outside Linux.
func readContainer() (time.Duration, int64, bool) { return 0, 0, false }
