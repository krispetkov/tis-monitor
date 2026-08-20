# tis-monitor

Keeps macOS's `TextInputSwitcher` alive on low-RAM Macs.

## Why

`TextInputSwitcher` is the system helper behind input-source switching (e.g.
Ctrl+Space). On a Mac with limited RAM (this was built for a 16GB machine), macOS
treats it as expendable under memory pressure and kills it. You can see if it's the same reason on your machine by executing the following command in your terminal: `launchctl print gui/501/com.apple.TextInputSwitcher`, and look for `last exit reason =
JETSAM_REASON_MEMORY_IDLE_EXIT`. It doesn't reliably come back on its own, so
Ctrl+Space silently stops working until something revives it.

## Solution

`tismonitor` is a tiny background daemon that notices and revives it — checked every
10 seconds, and instantly when you press left Ctrl (so it's usually back before you
finish pressing Space).

`TextInputSwitcher` process is itself a real on-demand LaunchAgent
(Mach-activated (see below), no `RunAtLoad`/`KeepAlive` of its own) — by design it exits when idle
under pressure and expects to be woken back up on demand. So `tismonitor` doesn't
relaunch the raw binary itself; it asks `launchd` to restart its own job be executing:

```bash
launchctl kickstart -k gui/<uid>/com.apple.TextInputSwitcher
```

Note: A raw relaunch would risk a duplicate process racing launchd's copy for the same service names. Also worth knowing: this process is protected at the OS level — neither
a plain `kill` nor `launchctl kill` from a normal user session can touch it, even from
its own owning user. `launchctl kickstart` is the one lever a regular account has.

## Requirements

- macOS
- Go 1.26+
- Xcode Command Line Tools (`xcode-select --install`) — needed for cgo, used here to
  call `CGEventSourceKeyState` for the left-Ctrl check.

## Build

```bash
go build -o tismonitor .
```

## Usage

```bash
./tismonitor status      # is TextInputSwitcher running? is tismonitor installed?
./tismonitor run         # run in the foreground (this is what autostart uses)
./tismonitor install     # install + start the autostart LaunchAgent
./tismonitor uninstall   # remove it
```

## Where things live

Running `install` copies the binary and sets up autostart:

| What | Path |
| --- | --- |
| Installed binary | `~/Library/Application Support/TISMonitor/tismonitor` |
| LaunchAgent | `~/Library/LaunchAgents/local.tismonitor.plist` |
| Logs (stdout) | `~/Library/Logs/tismonitor.log` |
| Logs (stderr/crashes) | `~/Library/Logs/tismonitor.err` |

The LaunchAgent is set to start at login and restart it if it ever exits
(`RunAtLoad`/`KeepAlive`), and runs as a background-priority process.

## Stopping / uninstalling

```bash
./tismonitor uninstall
```

This unloads the LaunchAgent, deletes the plist, and removes the installed binary and
its folder. Log files under `~/Library/Logs/tismonitor.*` are left in place — delete
them by hand if you want them gone too. After this, nothing of `tismonitor` remains
running or configured to start.

If you just want to stop it temporarily without a full uninstall:

```bash
launchctl bootout gui/$(id -u)/local.tismonitor
```

and start it again later with `./tismonitor install` (or `launchctl bootstrap` +
`launchctl kickstart -k` against the existing plist, if it's still there).

## Troubleshooting

- Check `~/Library/Logs/tismonitor.log` for what it's been doing.
- Manually test the revival command it uses:
  `launchctl kickstart -k gui/$(id -u)/com.apple.TextInputSwitcher`
- `./tismonitor status` shows both TextInputSwitcher's current state and whether the
  agent is installed/running.

## What is Mach

[Mach](https://en.wikipedia.org/wiki/Mach_(kernel)) is the low-level kernel underneath macOS (XNU = Mach kernel + BSD on top). One thing it provides is a messaging system for processes to talk to each other via named "ports" instead of files or sockets — that's called Mach IPC.

A "Mach service" is just a named port that something has registered, so other processes can look it up by name and connect. What makes this relevant here: **launchd can reserve a service name on behalf of a job without that job actually running yet**. It holds the name; the first time anything does a lookup on it, launchd notices and starts (or restarts) the real process to go answer it. That's "on-demand" / Mach-activation.

That's exactly TextInputSwitcher's setup — its plist reserves `com.apple.inputswitcher.running/.startup/.stop`, with no `RunAtLoad`. So it can exit quietly under memory pressure, and launchd is still sitting there holding those three names, ready to relaunch it whenever something asks for one of them.

Why this matters for the "don't just exec the binary yourself": if you manually relaunch the raw binary, that copy never tells launchd "I'm here" for those service names - launchd still thinks they're unclaimed. So if something later does a genuine lookup, launchd might start its own copy too, and now you have two processes for one job. `launchctl kickstart` avoids that because it works through launchd, which is the only thing that actually owns that registration.
