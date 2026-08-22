// Package browser opens a URL on whichever desktop this is running on.
package browser

import (
	"os"
	"os/exec"
	"runtime"
)

// Open shows a URL to whoever is at the machine, and says nothing when there
// is nobody there: every caller has already printed the URL for the case where
// this does nothing, because on a server, over ssh, or in a container it will.
func Open(url string) {
	// A machine where opening a window is wrong says so: a page driven from
	// somewhere else, or a run nobody is watching.
	if os.Getenv("PLAUD_NO_BROWSER") != "" {
		return
	}

	var command string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "windows":
		command, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		command = "xdg-open"
	}

	_ = exec.Command(command, append(args, url)...).Start()
}
