package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Lid and power: let a laptop keep serving with its lid closed.
//
// Why the usual manual fix often "doesn't work":
//   - logind has three separate lid settings (on battery, on power, docked);
//     people change one and the laptop still sleeps in another situation.
//   - Editing /etc/systemd/logind.conf does nothing until logind reloads.
//   - The line is often left commented out (#HandleLidSwitch=...).
//   - Something else can still suspend the machine (idle timers, power keys).
//
// Haven writes all three settings in a drop-in file, blocks every sleep
// target, reloads logind, and then reads back what logind actually uses.

const lidDropIn = "/etc/systemd/logind.conf.d/50-haven-lid.conf"

var sleepTargets = []string{"sleep.target", "suspend.target", "hibernate.target", "hybrid-sleep.target", "suspend-then-hibernate.target"}

type Battery struct {
	Percent  int    `json:"percent"`
	Status   string `json:"status"` // Charging, Discharging, Full, Not charging
	ACOnline bool   `json:"acOnline"`
}

type PowerStatus struct {
	Supported       bool     `json:"supported"`
	Reason          string   `json:"reason,omitempty"`
	Laptop          bool     `json:"laptop"`
	LidState        string   `json:"lidState,omitempty"` // open / closed
	KeepAwake       bool     `json:"keepAwake"`          // Haven's settings are in place
	LidOnBattery    string   `json:"lidOnBattery"`       // what logind really does
	LidOnPower      string   `json:"lidOnPower"`
	LidDocked       string   `json:"lidDocked"`
	SleepBlocked    bool     `json:"sleepBlocked"`
	Effective       bool     `json:"effective"` // closing the lid will NOT stop the server
	Desktop         bool     `json:"desktop"`   // a graphical desktop session is running
	ScreenSupported bool     `json:"screenSupported"`
	ScreenOff       bool     `json:"screenOff"`
	Battery         *Battery `json:"battery,omitempty"`
	Message         string   `json:"message,omitempty"`
}

func PowerState(ctx context.Context) PowerStatus {
	st := PowerStatus{}
	if !isLinux() {
		st.Reason = "Lid and power settings are only available on the Linux server."
		return st
	}
	if !hasCmd("busctl") || !hasCmd("systemctl") {
		st.Reason = "This server doesn't use systemd-logind, so Haven can't manage the lid."
		return st
	}
	st.Supported = true

	lids, _ := filepath.Glob("/proc/acpi/button/lid/*/state")
	for _, f := range lids {
		b, _ := os.ReadFile(f)
		// e.g. "state:      open"
		if fields := strings.Fields(string(b)); len(fields) > 0 {
			st.LidState = fields[len(fields)-1]
		}
	}
	st.Battery = readBattery()
	st.Laptop = len(lids) > 0 || st.Battery != nil

	st.LidOnBattery = logindProp(ctx, "HandleLidSwitch")
	st.LidOnPower = logindProp(ctx, "HandleLidSwitchExternalPower")
	st.LidDocked = logindProp(ctx, "HandleLidSwitchDocked")
	st.SleepBlocked = true
	for _, t := range sleepTargets {
		if link, err := os.Readlink(filepath.Join("/etc/systemd/system", t)); err != nil || link != "/dev/null" {
			st.SleepBlocked = false
		}
	}
	_, err := os.Stat(lidDropIn)
	st.KeepAwake = err == nil
	st.Effective = lidIgnored(st.LidOnBattery) && lidIgnored(st.LidOnPower) && lidIgnored(st.LidDocked) && st.SleepBlocked
	st.Desktop = hasGraphicalSession(ctx)

	bls, _ := filepath.Glob("/sys/class/backlight/*/bl_power")
	st.ScreenSupported = len(bls) > 0
	for _, f := range bls {
		if b, err := os.ReadFile(f); err == nil && strings.TrimSpace(string(b)) == "4" {
			st.ScreenOff = true
		}
	}
	return st
}

func lidIgnored(v string) bool { return v == "ignore" || v == "lock" }

// logindProp reads a setting from the running logind, e.g. `s "suspend"`.
func logindProp(ctx context.Context, name string) string {
	out, err := runHost(ctx, 5*time.Second, "busctl", "get-property", "org.freedesktop.login1",
		"/org/freedesktop/login1", "org.freedesktop.login1.Manager", name)
	if err != nil {
		return "unknown"
	}
	return parseBusctlString(out)
}

func parseBusctlString(out string) string {
	_, v, ok := strings.Cut(strings.TrimSpace(out), " ")
	if !ok {
		return "unknown"
	}
	return strings.Trim(v, `"`)
}

func hasGraphicalSession(ctx context.Context) bool {
	out, err := runHost(ctx, 5*time.Second, "loginctl", "list-sessions", "--no-legend")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		t, _ := runHost(ctx, 5*time.Second, "loginctl", "show-session", f[0], "-p", "Type", "--value")
		if t == "x11" || t == "wayland" || t == "mir" {
			return true
		}
	}
	return false
}

func readBattery() *Battery {
	var bat *Battery
	ac := false
	supplies, _ := filepath.Glob("/sys/class/power_supply/*")
	for _, s := range supplies {
		typ, _ := os.ReadFile(filepath.Join(s, "type"))
		switch strings.TrimSpace(string(typ)) {
		case "Battery":
			if scope, _ := os.ReadFile(filepath.Join(s, "scope")); strings.TrimSpace(string(scope)) == "Device" {
				continue // a mouse or keyboard battery, not the laptop's
			}
			capb, _ := os.ReadFile(filepath.Join(s, "capacity"))
			stb, _ := os.ReadFile(filepath.Join(s, "status"))
			pct, _ := strconv.Atoi(strings.TrimSpace(string(capb)))
			bat = &Battery{Percent: pct, Status: strings.TrimSpace(string(stb))}
		case "Mains", "USB":
			if on, _ := os.ReadFile(filepath.Join(s, "online")); strings.TrimSpace(string(on)) == "1" {
				ac = true
			}
		}
	}
	if bat != nil {
		bat.ACOnline = ac
	}
	return bat
}

// SetKeepAwake applies (or removes) the lid and sleep settings, reloads
// logind, and checks that they really took effect.
func SetKeepAwake(ctx context.Context, on bool) (PowerStatus, error) {
	if st := PowerState(ctx); !st.Supported {
		return st, errors.New(st.Reason)
	}
	if on {
		if err := os.MkdirAll(filepath.Dir(lidDropIn), 0o755); err != nil {
			return PowerState(ctx), err
		}
		conf := "# Written by Haven: keep serving with the lid closed.\n[Login]\nHandleLidSwitch=ignore\nHandleLidSwitchExternalPower=ignore\nHandleLidSwitchDocked=ignore\n"
		if err := writeFileAtomic(lidDropIn, []byte(conf), 0o644); err != nil {
			return PowerState(ctx), err
		}
		if _, err := runHost(ctx, 20*time.Second, "systemctl", append([]string{"mask"}, sleepTargets...)...); err != nil {
			return PowerState(ctx), err
		}
	} else {
		if err := os.Remove(lidDropIn); err != nil && !os.IsNotExist(err) {
			return PowerState(ctx), err
		}
		if _, err := runHost(ctx, 20*time.Second, "systemctl", append([]string{"unmask"}, sleepTargets...)...); err != nil {
			return PowerState(ctx), err
		}
	}

	// Ask logind to re-read its settings. If that isn't enough, restart it,
	// but only when no desktop session is open (restarting could end it).
	_, _ = runHost(ctx, 10*time.Second, "systemctl", "kill", "-s", "HUP", "systemd-logind")
	time.Sleep(1500 * time.Millisecond)
	st := PowerState(ctx)
	applied := func(s PowerStatus) bool {
		if on {
			return lidIgnored(s.LidOnBattery) && lidIgnored(s.LidOnPower)
		}
		return !lidIgnored(s.LidOnBattery)
	}
	if !applied(st) && !st.Desktop {
		_, _ = runHost(ctx, 20*time.Second, "systemctl", "restart", "systemd-logind")
		time.Sleep(1500 * time.Millisecond)
		st = PowerState(ctx)
	}
	switch {
	case applied(st) && on:
		st.Message = "Done. Closing the lid no longer stops the server, and it will never go to sleep."
	case applied(st):
		st.Message = "Done. The laptop sleeps normally again when the lid closes."
	default:
		st.Message = "Saved, but the system hasn't picked it up yet. Restart the laptop once to finish."
	}
	return st, nil
}

// SetScreenOff turns the built-in screen's backlight off (4) or on (0).
func SetScreenOff(off bool) error {
	bls, _ := filepath.Glob("/sys/class/backlight/*/bl_power")
	if len(bls) == 0 {
		return errors.New("this server has no screen backlight Haven can control")
	}
	val := "0"
	if off {
		val = "4"
	}
	var lastErr error
	for _, f := range bls {
		if err := os.WriteFile(f, []byte(val), 0o644); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
