package platform

import (
	"context"
	"testing"
)

func TestDetectorEnvironmentFlagsThreeState(t *testing.T) {
	ctx := context.Background()
	osRelease := "/etc/os-release"

	t.Run("inconclusive when probes fail", func(t *testing.T) {
		d := fixtureDetector(map[string]string{
			osRelease: "ID=ubuntu\nNAME=Ubuntu\nVERSION_ID=24.04\n",
		}, nil, baseCommands(), nil)
		facts := d.detect(ctx)
		if facts.IsWSL || facts.IsContainer {
			t.Fatalf("environment traits = wsl %v container %v, want false", facts.IsWSL, facts.IsContainer)
		}
		if !facts.IsWSLUnknown || !facts.IsContainerUnknown {
			t.Fatalf("unknown flags = wsl %v container %v, want both true", facts.IsWSLUnknown, facts.IsContainerUnknown)
		}
	})

	t.Run("conclusive negative when probes complete", func(t *testing.T) {
		d := fixtureDetector(map[string]string{
			osRelease:           "ID=ubuntu\nNAME=Ubuntu\nVERSION_ID=24.04\n",
			"/proc/version":     "Linux version 6.12.0",
			"/proc/self/cgroup": "0::/",
		}, nil, baseCommands(), nil)
		facts := d.detect(ctx)
		if facts.IsWSLUnknown || facts.IsContainerUnknown {
			t.Fatalf("unknown flags = wsl %v container %v, want both false", facts.IsWSLUnknown, facts.IsContainerUnknown)
		}
		if facts.IsWSL || facts.IsContainer {
			t.Fatalf("environment traits = wsl %v container %v, want false", facts.IsWSL, facts.IsContainer)
		}
	})

	t.Run("wsl positive from kernel when proc version is unreadable", func(t *testing.T) {
		commands := baseCommands()
		commands["uname -r"] = "5.15.90.1-microsoft-standard-WSL2\n"
		d := fixtureDetector(map[string]string{
			osRelease: "ID=ubuntu\nNAME=Ubuntu\nVERSION_ID=24.04\n",
		}, nil, commands, nil)
		facts := d.detect(ctx)
		if !facts.IsWSL || facts.IsWSLUnknown {
			t.Fatalf("wsl = %v unknown = %v, want a conclusive positive", facts.IsWSL, facts.IsWSLUnknown)
		}
	})

	t.Run("container positive is conclusive", func(t *testing.T) {
		d := fixtureDetector(map[string]string{
			osRelease:     "ID=alpine\nNAME=Alpine\nVERSION_ID=3.20\n",
			"/.dockerenv": "",
		}, nil, baseCommands(), nil)
		facts := d.detect(ctx)
		if !facts.IsContainer || facts.IsContainerUnknown {
			t.Fatalf("container = %v unknown = %v, want a conclusive positive", facts.IsContainer, facts.IsContainerUnknown)
		}
	})

	t.Run("non-linux is a conclusive negative", func(t *testing.T) {
		commands := baseCommands()
		commands["uname -s"] = "Darwin\n"
		commands["uname -r"] = "24.0.0\n"
		d := fixtureDetector(nil, nil, commands, nil)
		facts := d.detect(ctx)
		if facts.OS != "darwin" {
			t.Fatalf("os = %q, want darwin", facts.OS)
		}
		if facts.IsWSLUnknown || facts.IsContainerUnknown {
			t.Fatalf("unknown flags = wsl %v container %v, want both false on non-linux", facts.IsWSLUnknown, facts.IsContainerUnknown)
		}
	})
}
