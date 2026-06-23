package seal

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tooppoo/git-kura/internal/gitutil"
)

const DefaultLockTimeout = 5 * time.Second
const MaxLockTimeout = time.Hour
const MaxLockTimeoutMs = int64(MaxLockTimeout / time.Millisecond)
const LockInterval = 100 * time.Millisecond
const LockTimeoutConfigKey = "kura.sealLockTimeoutMs"

// LockTimeoutErr is returned when the seal store lock cannot be acquired within
// the configured timeout.
type LockTimeoutErr struct{ Timeout time.Duration }

func (e LockTimeoutErr) Error() string {
	return fmt.Sprintf("seal-lock-timeout: failed to acquire seal store lock after %s", e.Timeout)
}

// ResolveLockTimeout determines the seal store lock timeout from the
// kura.sealLockTimeoutMs Git config value, falling back to defaultLockTimeout.
func ResolveLockTimeout(repoRoot string) (time.Duration, error) {
	raw, configured, err := gitutil.ConfigValue(repoRoot, LockTimeoutConfigKey)
	if err != nil {
		return 0, err
	}
	if !configured {
		return DefaultLockTimeout, nil
	}
	value := strings.TrimRight(raw, "\n")
	if !isDecimalDigits(value) {
		return 0, fmt.Errorf("invalid %s %q: expected a non-negative integer number of milliseconds", LockTimeoutConfigKey, value)
	}
	ms, err := strconv.ParseInt(value, 10, 64)
	if err != nil || ms > MaxLockTimeoutMs {
		return 0, fmt.Errorf("invalid %s %q: must not exceed %d milliseconds (%s)", LockTimeoutConfigKey, value, MaxLockTimeoutMs, MaxLockTimeout)
	}
	return time.Duration(ms) * time.Millisecond, nil
}

// AcquireLock creates the lock file using atomic O_CREATE|O_EXCL, retrying
// until the supplied timeout elapses. Returns a release function that removes
// the lock file. A zero timeout makes exactly one attempt.
func AcquireLock(lockPath string, timeout time.Duration) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create seal store dir: %w", err)
	}
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			return func() {
				if removeErr := os.Remove(lockPath); removeErr != nil && !os.IsNotExist(removeErr) {
					fmt.Fprintf(os.Stderr,
						"warning: failed to release seal store lock %s: %v\nremove the file manually or subsequent seal commands will time out\n",
						lockPath, removeErr)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquire seal store lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, LockTimeoutErr{Timeout: timeout}
		}
		time.Sleep(LockInterval)
	}
}
