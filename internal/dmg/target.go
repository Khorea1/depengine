package dmg

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
)

// appName returns the declared .app bundle name from the method config.
func appName(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	value, _ := mc.Config["app"].(string)
	return strings.TrimSpace(value)
}

// scopeValue returns the portable scope spelling; "" means user default.
// The contract restricts the value to "user"/"system".
func scopeValue(mc *config.MethodCandidate) string {
	if mc == nil {
		return ""
	}
	value, _ := mc.Config["scope"].(string)
	return strings.TrimSpace(value)
}

func systemScope(mc *config.MethodCandidate) bool {
	return scopeValue(mc) == "system"
}

// appRoot maps the portable scope to the Applications directory where the
// copied bundle is owned: user scope (and the historical default) is the
// operator's personal Applications folder, system scope is /Applications.
func appRoot(mc *config.MethodCandidate) (string, error) {
	if systemScope(mc) {
		return "/Applications", nil
	}
	home := homeDir()
	if home == "" {
		return "", fmt.Errorf("dmg: cannot resolve home directory for user scope")
	}
	return filepath.Join(home, "Applications"), nil
}

// appTarget returns the exact owned destination of the declared bundle.
// The app name is revalidated here so Observe/Remove can never diverge from
// Install's strict payload contract.
func appTarget(mc *config.MethodCandidate) (string, error) {
	app := appName(mc)
	if err := validateAppName(app); err != nil {
		return "", err
	}
	root, err := appRoot(mc)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, app), nil
}

// validateAppName enforces the strict bundle-name shape used everywhere:
// a plain .app name with no separators, no dot-prefix, and no parent refs.
func validateAppName(app string) error {
	if !strings.HasSuffix(app, ".app") {
		return fmt.Errorf("dmg: app %q must name a .app bundle", app)
	}
	if strings.Contains(app, "/") || strings.HasPrefix(app, ".") || app == ".app" || app == ".." {
		return fmt.Errorf("dmg: app %q must be a plain .app bundle name", app)
	}
	return nil
}

func homeDir() string {
	if home := os.Getenv("HOME"); home != "" {
		return home
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}
