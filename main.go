//go:build darwin

package main

/*
#cgo LDFLAGS: -framework ApplicationServices
#include <ApplicationServices/ApplicationServices.h>

static const CGKeyCode kLeftControlKeyCode = 0x3B;

static int leftControlIsDown() {
    return CGEventSourceKeyState(kCGEventSourceStateHIDSystemState, kLeftControlKeyCode) ? 1 : 0;
}
*/
import "C"

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	tisProcessName   = "TextInputSwitcher"
	tisAgentLabel    = "com.apple.TextInputSwitcher"
	agentLabel       = "local.tis-monitor"
	pollInterval     = 50 * time.Millisecond
	periodicInterval = 10 * time.Second
)

func main() {
	cmd := "run"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "run":
		runDaemon()
	case "install":
		install()
	case "uninstall":
		uninstall()
	case "status":
		status()
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [run|install|uninstall|status]\n", filepath.Base(os.Args[0]))
		os.Exit(1)
	}
}

// runDaemon is the long-running loop behind the "run" subcommand: it revives
// TextInputSwitcher once up front, then polls on every tick until killed.
func runDaemon() {
	log.SetOutput(os.Stdout) // routine logs -> tis-monitor.log; tis-monitor.err stays for real crashes
	log.Printf("tis-monitor starting (pid %d)", os.Getpid())
	ensureRunning()

	var detector ctrlEdgeDetector
	lastPeriodic := time.Now()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for range ticker.C {
		tick(&detector, &lastPeriodic)
	}
}

// ensureRunning revives TextInputSwitcher if it isn't currently running;
// it's a no-op when it's already alive.
func ensureRunning() {
	if isTisRunning() {
		return
	}
	log.Printf("%s not running, reviving", tisProcessName)
	revive()
}

// isTisRunning reports whether the TextInputSwitcher process is currently alive.
func isTisRunning() bool {
	return exec.Command("pgrep", "-x", tisProcessName).Run() == nil
}

// revive asks launchd to (re)start its own TextInputSwitcher job. We go through
// launchctl rather than launching the binary ourselves because launchd owns that
// process's lifecycle (Mach service registration, jetsam priority, etc.) — and
// because macOS refuses a plain kill/launchctl-kill against it from a normal
// user session, so kickstart is the only lever we actually have.
func revive() {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), tisAgentLabel)
	out, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput()
	if err != nil {
		log.Printf("revive: launchctl kickstart failed: %v (%s)", err, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("revive: kickstarted %s", tisAgentLabel)
}

// ctrlEdgeDetector reports a rising edge exactly once per key press, not on
// every poll tick while the key stays held down.
type ctrlEdgeDetector struct {
	wasDown bool
}

// Update records the current raw key state and reports true only on the
// up-to-down transition — once per press, no matter how long it's then held.
func (d *ctrlEdgeDetector) Update(down bool) bool {
	rising := down && !d.wasDown
	d.wasDown = down
	return rising
}

// leftControlDown reports the current raw hardware state of the left Control
// key: true for the entire time it's held, not just on the initial press.
// ctrlEdgeDetector is what turns this into a one-shot press event.
func leftControlDown() bool {
	return C.leftControlIsDown() != 0
}

// tick runs one poll iteration, recovering from any panic so a single bad
// tick can never take the whole daemon down (KeepAlive in the LaunchAgent
// assumes this process never exits on its own).
func tick(detector *ctrlEdgeDetector, lastPeriodic *time.Time) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic in monitor loop: %v", r)
		}
	}()

	if detector.Update(leftControlDown()) {
		ensureRunning()
	}
	if time.Since(*lastPeriodic) >= periodicInterval {
		*lastPeriodic = time.Now()
		ensureRunning()
	}
}

// status prints whether TextInputSwitcher is running and whether the
// tis-monitor LaunchAgent itself is installed and running.
func status() {
	if isTisRunning() {
		fmt.Println("TextInputSwitcher: running")
	} else {
		fmt.Println("TextInputSwitcher: NOT running")
	}

	out, err := exec.Command("launchctl", "print", fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel)).CombinedOutput()
	if err != nil {
		fmt.Println("tis-monitor agent: not installed")
		return
	}
	if strings.Contains(string(out), "state = running") {
		fmt.Println("tis-monitor agent: installed and running")
	} else {
		fmt.Println("tis-monitor agent: installed but not running")
	}
}
