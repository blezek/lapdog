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
		// Rollback restored the old target. Bring it back so collection resumes
		// and the persisted failure is visible in the interface.
		if startErr := startInstalled(h.Target); startErr != nil {
			return fmt.Errorf("apply failed; rollback succeeded, but the old executable could not restart: %v; restart: %w", err, startErr)
		}
		return fmt.Errorf("apply failed; rollback succeeded: %w", err)
	}
	if err := startInstalled(h.Target); err != nil {
		return fmt.Errorf("updated executable installed but could not be started: %w", err)
	}
	return nil
}

// RecordHandoffFailure preserves a helper-process failure for the next normal
// process and the update popdown.
func RecordHandoffFailure(path string, failure error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var p persisted
	if json.Unmarshal(b, &p) != nil {
		return
	}
	p.Pending = true
	p.Error = failure.Error()
	_ = atomicJSON(path, p)
}
