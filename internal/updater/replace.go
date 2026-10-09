package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	selfreplace "github.com/creativeprojects/go-selfupdate/update"
)

func launchProcess(path string, args ...string) error { return exec.Command(path, args...).Start() }

var (
	applyReplacement = selfreplace.Apply
	rollbackError    = selfreplace.RollbackError
	startInstalled   = func(path string) error { return exec.Command(path).Start() }
)

// HandoffArgs describes the internal replacement invocation. User-facing
// command lines never need these flags.
type HandoffArgs struct {
	PID                       int
	Target, Backup, StatePath string
}

// ParseHandoff returns ok only for the complete internal argument set.
func ParseHandoff(args []string) (HandoffArgs, bool, error) {
	if len(args) == 0 || args[0] != "--wait-for-pid" {
		return HandoffArgs{}, false, nil
	}
	if len(args) != 8 || args[2] != "--replace" || args[4] != "--backup" || args[6] != "--update-state" {
		return HandoffArgs{}, true, errors.New("invalid internal update arguments")
	}
	pid, err := strconv.Atoi(args[1])
	if err != nil || pid <= 0 {
		return HandoffArgs{}, true, errors.New("invalid wait PID")
	}
	return HandoffArgs{PID: pid, Target: args[3], Backup: args[5], StatePath: args[7]}, true, nil
}

// RunHandoff waits for the old process, applies the already verified executable
// with rollback support, and starts the installed path.
func RunHandoff(h HandoffArgs) error {
	deadline := time.Now().Add(30 * time.Second)
	for processExists(h.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if processExists(h.PID) {
		return errors.New("old LapDog process did not exit")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	f, err := os.Open(self)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := applyReplacement(f, selfreplace.Options{TargetPath: h.Target, OldSavePath: h.Backup}); err != nil {
		if rollback := rollbackError(err); rollback != nil {
			return fmt.Errorf("apply failed and rollback failed: %v; rollback: %w", err, rollback)
		}
		// Record the failure before starting the old executable, so its startup
		// cannot miss the reason and automatically attempt the same handoff.
		return restartOldAfterFailure(h, fmt.Errorf("apply failed; rollback succeeded: %w", err))
	}
	if err := startInstalled(h.Target); err != nil {
		if rollback := restoreBackup(h.Target, h.Backup); rollback != nil {
			return fmt.Errorf("updated executable could not start: %v; rollback failed: %w", err, rollback)
		}
		return restartOldAfterFailure(h, fmt.Errorf("updated executable could not start; old executable restored: %w", err))
	}
	return nil
}

func restartOldAfterFailure(h HandoffArgs, failure error) error {
	if err := RecordHandoffFailure(h.StatePath, failure); err != nil {
		// Starting the old executable with an unrecorded acceptance would let it
		// attempt the same failed replacement again, indefinitely.
		return fmt.Errorf("%v; could not record failure, old executable was not restarted: %w", failure, err)
	}
	if err := startInstalled(h.Target); err != nil {
		return fmt.Errorf("%v; old executable could not restart: %w", failure, err)
	}
	return failure
}

// restoreBackup keeps the failed replacement recoverable until the old binary
// has been moved back to its installed path.
func restoreBackup(target, backup string) error {
	failed := target + ".failed"
	if err := os.Remove(failed); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove earlier failed executable: %w", err)
	}
	if err := os.Rename(target, failed); err != nil {
		return fmt.Errorf("move failed executable aside: %w", err)
	}
	if err := os.Rename(backup, target); err != nil {
		if restoreErr := os.Rename(failed, target); restoreErr != nil {
			return fmt.Errorf("restore old executable: %v; restore failed replacement: %w", err, restoreErr)
		}
		return fmt.Errorf("restore old executable: %w", err)
	}
	_ = os.Remove(failed)
	return nil
}

// RecordHandoffFailure preserves a helper-process failure for the next normal
// process and the update popdown.
func RecordHandoffFailure(path string, failure error) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read update state: %w", err)
	}
	var p persisted
	if err := json.Unmarshal(b, &p); err != nil {
		return fmt.Errorf("decode update state: %w", err)
	}
	// The helper records a failure before restarting the old executable. The
	// main entrypoint also reports its returned error; that second report must
	// not turn Pending back on after the old process consumed the failure.
	if p.Error == failure.Error() {
		return nil
	}
	p.Pending = true
	p.Error = failure.Error()
	if err := atomicJSON(path, p); err != nil {
		return fmt.Errorf("save update failure: %w", err)
	}
	return nil
}
