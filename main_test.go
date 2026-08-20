//go:build darwin

package main

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCtrlEdgeDetectorFiresOnceOnRisingEdge(t *testing.T) {
	var d ctrlEdgeDetector
	sequence := []bool{false, false, true, true, false, true, true, false}
	wantEdges := []bool{false, false, true, false, false, true, false, false}

	for i, down := range sequence {
		got := d.Update(down)
		if got != wantEdges[i] {
			t.Errorf("step %d: Update(%v) = %v, want %v", i, down, got, wantEdges[i])
		}
	}
}

func TestCtrlEdgeDetectorStartsUnpressed(t *testing.T) {
	var d ctrlEdgeDetector
	if d.Update(false) {
		t.Error("Update(false) on a fresh detector should not report an edge")
	}
}

func TestRenderLaunchAgentPlistContainsExpectedFields(t *testing.T) {
	plist := renderLaunchAgentPlist(launchAgentConfig{
		Label:      "local.tismonitor",
		BinaryPath: "/tmp/tismonitor",
		StdoutPath: "/tmp/tismonitor.log",
		StderrPath: "/tmp/tismonitor.err",
	})

	for _, want := range []string{
		"<string>local.tismonitor</string>",
		"<string>/tmp/tismonitor</string>",
		"<string>run</string>",
		"<string>/tmp/tismonitor.log</string>",
		"<string>/tmp/tismonitor.err</string>",
		"<key>RunAtLoad</key>",
		"<key>KeepAlive</key>",
	} {
		if !strings.Contains(plist, want) {
			t.Errorf("rendered plist missing %q\n%s", want, plist)
		}
	}
}

func TestRenderLaunchAgentPlistIsValidPlist(t *testing.T) {
	plist := renderLaunchAgentPlist(launchAgentConfig{
		Label:      "local.tismonitor",
		BinaryPath: "/tmp/tismonitor",
		StdoutPath: "/tmp/tismonitor.log",
		StderrPath: "/tmp/tismonitor.err",
	})

	cmd := exec.Command("plutil", "-lint", "-")
	cmd.Stdin = strings.NewReader(plist)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plutil -lint rejected generated plist: %v\n%s", err, out)
	}
}
