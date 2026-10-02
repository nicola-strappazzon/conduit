// Package browser opens URLs in the system browser, optionally forcing a
// specific Chrome profile. It has no knowledge of AWS, SSO, or any other
// caller concern — just "open this URL, maybe in this profile".
package browser

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Open opens url in the system browser. If chromeProfile is set, it forces
// the URL to open in that Chrome profile (matched by profile directory,
// display name, or account email against Chrome's Local State file)
// instead of whatever the OS default browser/profile is.
func Open(url, chromeProfile string) error {
	if chromeProfile != "" {
		dir := resolveChromeProfileDir(chromeProfile)
		switch runtime.GOOS {
		case "darwin":
			return exec.Command("open", "-na", "Google Chrome", "--args", "--profile-directory="+dir, url).Start()
		case "linux":
			return exec.Command("google-chrome", "--profile-directory="+dir, url).Start()
		default:
			return fmt.Errorf("chrome profile selection is not supported on %s", runtime.GOOS)
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// resolveChromeProfileDir maps a Chrome profile's directory name, display
// name, or account email to its actual profile directory (e.g. "Profile 7"),
// by reading Chrome's Local State file. If it can't resolve a match, it
// falls back to treating the input as the directory name itself, so passing
// the real directory name (e.g. "Profile 7") always works even without this
// lookup succeeding.
func resolveChromeProfileDir(nameOrEmail string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nameOrEmail
	}

	data, err := os.ReadFile(filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "Local State"))
	if err != nil {
		return nameOrEmail
	}

	var state struct {
		Profile struct {
			InfoCache map[string]struct {
				Name     string `json:"name"`
				UserName string `json:"user_name"`
				GaiaName string `json:"gaia_name"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return nameOrEmail
	}

	target := strings.ToLower(nameOrEmail)
	for dir, info := range state.Profile.InfoCache {
		if strings.EqualFold(dir, nameOrEmail) ||
			strings.ToLower(info.Name) == target ||
			strings.ToLower(info.UserName) == target ||
			strings.ToLower(info.GaiaName) == target {
			return dir
		}
	}
	return nameOrEmail
}
