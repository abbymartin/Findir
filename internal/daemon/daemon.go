package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func RestartIfRunning(pidPath, dbPath, journalPath string) error {
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		return nil // no daemon running
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		os.Remove(pidPath)
		return nil
	}

	// Check if process is actually running
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		return nil
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(pidPath)
		return nil
	}

	// Stop the daemon
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("stopping daemon: %w", err)
	}

	// Wait briefly for it to exit
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}
	os.Remove(pidPath)

	// Start it again
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding executable: %w", err)
	}
	watcherPath := filepath.Join(filepath.Dir(exePath), "watcher")
	if _, err := os.Stat(watcherPath); err != nil {
		return nil // watcher binary not built, skip
	}

	cmd := exec.Command(watcherPath, dbPath, journalPath)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("restarting daemon: %w", err)
	}
	cmd.Process.Release()

	return nil
}
