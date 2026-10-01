package cli

import (
	"fmt"
	"os/exec"
	"runtime"
)

// defaultOpenBrowser launches url in the pilot's default browser, used
// when Config.OpenBrowser is unset (the real `login` run, never in
// tests). It is best-effort: `login`'s caller always has the printed
// consent URL to fall back to, so a launch failure here is not fatal.
func defaultOpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening browser: %w", err)
	}
	return nil
}
