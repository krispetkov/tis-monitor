//go:build darwin

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// getInstallDir returns the per-user directory tismonitor installs its binary into.
func getInstallDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, "Library", "Application Support", "TISMonitor")
}

// getInstalledBinaryPath returns where the binary lives once "install" has run.
func getInstalledBinaryPath() string {
	return filepath.Join(getInstallDir(), "tismonitor")
}

// getPlistPath returns the LaunchAgent plist path used for autostart.
func getPlistPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", agentLabel+".plist")
}

// getLogPaths returns the stdout/stderr file paths the installed LaunchAgent logs to.
func getLogPaths() (stdout, stderr string) {
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatalf("cannot determine home directory: %v", err)
	}
	dir := filepath.Join(home, "Library", "Logs")
	return filepath.Join(dir, "tismonitor.log"), filepath.Join(dir, "tismonitor.err")
}

// launchAgentConfig holds the values substituted into the generated LaunchAgent plist.
type launchAgentConfig struct {
	Label      string
	BinaryPath string
	StdoutPath string
	StderrPath string
}

// renderLaunchAgentPlist builds the LaunchAgent plist XML for cfg.
//
// Note: This has to be generated rather than a static file.
// launchd plists need fully-resolved absolute paths (no "~", no env expansion),
// and those paths depend on the installing user's home directory and chosen install location.
func renderLaunchAgentPlist(cfg launchAgentConfig) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, cfg.Label, cfg.BinaryPath, cfg.StdoutPath, cfg.StderrPath)
}

// copyFile copies src to dst, used to install the running binary into its target location.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

// install:
// - copies the current binary into installDir()
// - ad-hoc signs it
// - writes the LaunchAgent plist
// - loads it via launchctl so tismonitor autostarts at login and starts running immediately
func install() {
	self, err := os.Executable()
	if err != nil {
		log.Fatalf("install: cannot determine own path: %v", err)
	}

	dir := getInstallDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatalf("install: %v", err)
	}

	dst := getInstalledBinaryPath()
	if err := copyFile(self, dst); err != nil {
		log.Fatalf("install: copying binary: %v", err)
	}

	if out, err := exec.Command("codesign", "--force", "--sign", "-", "--identifier", agentLabel, dst).CombinedOutput(); err != nil {
		log.Printf("install: warning: codesign failed, continuing anyway: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	stdoutPath, stderrPath := getLogPaths()
	if err := os.MkdirAll(filepath.Dir(stdoutPath), 0o755); err != nil {
		log.Fatalf("install: creating log dir: %v", err)
	}

	plist := renderLaunchAgentPlist(launchAgentConfig{
		Label:      agentLabel,
		BinaryPath: dst,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
	})
	if err := os.WriteFile(getPlistPath(), []byte(plist), 0o644); err != nil {
		log.Fatalf("install: writing plist: %v", err)
	}

	domain := fmt.Sprintf("gui/%d", os.Getuid())
	target := fmt.Sprintf("%s/%s", domain, agentLabel)

	_ = exec.Command("launchctl", "bootout", target).Run() // fine if it wasn't loaded

	if out, err := exec.Command("launchctl", "bootstrap", domain, getPlistPath()).CombinedOutput(); err != nil {
		log.Fatalf("install: launchctl bootstrap failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("launchctl", "enable", target).CombinedOutput(); err != nil {
		log.Printf("install: warning: launchctl enable failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("launchctl", "kickstart", "-k", target).CombinedOutput(); err != nil {
		log.Printf("install: warning: launchctl kickstart failed: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf("Installed to %s\n", dst)
	fmt.Printf("LaunchAgent: %s\n", getPlistPath())
	fmt.Printf("Logs: %s / %s\n", stdoutPath, stderrPath)
}

// uninstall is undoing everything install did:
// - stops and removes the LaunchAgent
// - deletes the installed binary
func uninstall() {
	target := fmt.Sprintf("gui/%d/%s", os.Getuid(), agentLabel)

	if out, err := exec.Command("launchctl", "bootout", target).CombinedOutput(); err != nil {
		log.Printf("uninstall: launchctl bootout: %v (%s)", err, strings.TrimSpace(string(out)))
	}

	if err := os.Remove(getPlistPath()); err != nil && !os.IsNotExist(err) {
		log.Printf("uninstall: removing plist: %v", err)
	}
	if err := os.RemoveAll(getInstallDir()); err != nil {
		log.Printf("uninstall: removing installed binary: %v", err)
	}

	fmt.Println("Uninstalled: LaunchAgent removed and installed binary deleted.")
	fmt.Println("Log files under ~/Library/Logs/tismonitor.* were left in place; remove manually if you want.")
}
