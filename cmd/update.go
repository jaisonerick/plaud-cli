package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type updateState struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
}

func updateStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "plaud", "update-state.json"), nil
}

func loadUpdateState() *updateState {
	p, err := updateStatePath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var s updateState
	if json.Unmarshal(data, &s) != nil {
		return nil
	}
	return &s
}

func saveUpdateState(s *updateState) {
	p, err := updateStatePath()
	if err != nil {
		return
	}
	data, _ := json.Marshal(s)
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, data, 0600)
}

func fetchLatestRelease() (*ghRelease, error) {
	resp, err := http.Get("https://api.github.com/repos/jaisonerick/plaud-cli/releases/latest")
	if err != nil {
		return nil, fmt.Errorf("fetching latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parsing release: %w", err)
	}
	return &release, nil
}

// CheckForUpdate checks if a newer version is available, at most once per day.
// Prints a notice to stderr if an update is available. Errors are silently ignored.
func CheckForUpdate() {
	// A build from source (`dev`, or a git-describe string like 0.6.1-3-gabc1234)
	// is ahead of the latest release, not behind it.
	current := strings.TrimPrefix(Version, "v")
	if current == "dev" || strings.Contains(current, "-g") {
		return
	}

	state := loadUpdateState()
	if state != nil && time.Since(state.CheckedAt) < 24*time.Hour {
		// Use cached result
		notifyUpdate(current, strings.TrimPrefix(state.LatestVersion, "v"))
		return
	}

	release, err := fetchLatestRelease()
	if err != nil {
		return
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	saveUpdateState(&updateState{
		CheckedAt:     time.Now(),
		LatestVersion: latest,
	})

	notifyUpdate(current, latest)
}

func notifyUpdate(current, latest string) {
	if !newerThan(latest, current) {
		return
	}
	fmt.Fprintf(os.Stderr, "\nA new version of plaud is available: v%s → v%s\nRun `plaud update` to upgrade.\n", current, latest)
}

// replaceBinary puts the new binary where this one runs from.
//
// Windows refuses to overwrite a file that is running, but it will rename one:
// the running program keeps its handle and the name is freed for the new
// binary. What is moved aside is deleted by the next run, which is the first
// moment nothing holds it. Elsewhere a rename is enough, except for a path
// only root can write, and that is what sudo is for.
func replaceBinary(execPath, tmpPath string, rename func(string, string) error) error {
	if err := rename(tmpPath, execPath); err == nil {
		return nil
	}

	if runtime.GOOS != "windows" {
		sudoCmd := exec.Command("sudo", "cp", tmpPath, execPath)
		sudoCmd.Stdin, sudoCmd.Stdout, sudoCmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := sudoCmd.Run(); err != nil {
			return fmt.Errorf("replacing the binary, even with sudo: %w", err)
		}
		os.Remove(tmpPath)
		return nil
	}

	return replaceByMovingAside(execPath, tmpPath, rename)
}

// replaceByMovingAside frees the name of a binary that cannot be written over.
func replaceByMovingAside(execPath, tmpPath string, rename func(string, string) error) error {
	aside := oldBinaryPath(execPath)
	os.Remove(aside)
	if err := rename(execPath, aside); err != nil {
		return fmt.Errorf("moving the running binary aside: %w", err)
	}
	if err := rename(tmpPath, execPath); err != nil {
		// Better the old binary back than none at all.
		rename(aside, execPath)
		return fmt.Errorf("putting the new binary in place: %w", err)
	}
	return nil
}

// oldBinaryPath is where the running binary waits to be deleted, one update
// behind.
func oldBinaryPath(execPath string) string {
	return execPath + ".old"
}

// sweepOldBinary drops what an update left behind, which is nothing on the run
// that made it and a file on every run after.
func sweepOldBinary() {
	execPath, err := os.Executable()
	if err != nil {
		return
	}
	sweepOldBinaryAt(execPath)
}

func sweepOldBinaryAt(execPath string) {
	os.Remove(oldBinaryPath(execPath))
}

// newerThan reports whether one release is later than another. The check is
// what keeps a stale answer quiet: the last one is remembered for a day, so a
// machine that upgrades meanwhile would otherwise be told to move to the
// version it just left.
func newerThan(release, than string) bool {
	a, b := strings.Split(release, "."), strings.Split(than, ".")
	for i := 0; i < len(a) || i < len(b); i++ {
		if x, y := versionPart(a, i), versionPart(b, i); x != y {
			return x > y
		}
	}
	return false
}

// versionPart is one dotted number, with anything unreadable sorting older so
// that a version nobody can compare never nags.
func versionPart(fields []string, i int) int {
	if i >= len(fields) {
		return 0
	}
	n, err := strconv.Atoi(fields[i])
	if err != nil {
		return -1
	}
	return n
}

func init() {
	rootCmd.AddCommand(updateCmd)
}

func assetName() string {
	name := fmt.Sprintf("plaud-cli_%s_%s", runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update plaud to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Fetch latest release
		release, err := fetchLatestRelease()
		if err != nil {
			return err
		}

		// 2. Compare versions
		latest := strings.TrimPrefix(release.TagName, "v")

		saveUpdateState(&updateState{
			CheckedAt:     time.Now(),
			LatestVersion: latest,
		})

		if latest == Version {
			fmt.Printf("Already up to date (v%s)\n", Version)
			return nil
		}

		// 3. Find the right asset
		name := assetName()
		var downloadURL string
		for _, a := range release.Assets {
			if a.Name == name {
				downloadURL = a.BrowserDownloadURL
				break
			}
		}
		if downloadURL == "" {
			return fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		fmt.Printf("Updating v%s → v%s ...\n", Version, latest)

		// 4. Download binary
		dlResp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading release: %w", err)
		}
		defer dlResp.Body.Close()

		if dlResp.StatusCode != http.StatusOK {
			return fmt.Errorf("download returned %d", dlResp.StatusCode)
		}

		newBinary, err := io.ReadAll(dlResp.Body)
		if err != nil {
			return fmt.Errorf("reading binary: %w", err)
		}

		// 5. Replace self
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current executable: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("resolving executable path: %w", err)
		}

		// Write to a temp file first
		tmpFile, err := os.CreateTemp(filepath.Dir(execPath), "plaud-update-*")
		if err != nil {
			return fmt.Errorf("creating temp file: %w", err)
		}
		tmpPath := tmpFile.Name()

		if _, err := tmpFile.Write(newBinary); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("writing new binary: %w", err)
		}
		tmpFile.Close()
		os.Chmod(tmpPath, 0755)

		if err := replaceBinary(execPath, tmpPath, os.Rename); err != nil {
			os.Remove(tmpPath)
			return err
		}

		fmt.Printf("Updated to v%s\n", latest)
		return nil
	},
}
