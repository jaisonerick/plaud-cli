package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	if Version == "dev" {
		return
	}

	state := loadUpdateState()
	if state != nil && time.Since(state.CheckedAt) < 24*time.Hour {
		// Use cached result
		if state.LatestVersion != "" && state.LatestVersion != Version {
			fmt.Fprintf(os.Stderr, "\nA new version of plaud is available: v%s → v%s\nRun `plaud update` to upgrade.\n", Version, state.LatestVersion)
		}
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

	if latest != Version {
		fmt.Fprintf(os.Stderr, "\nA new version of plaud is available: v%s → v%s\nRun `plaud update` to upgrade.\n", Version, latest)
	}
}

func init() {
	rootCmd.AddCommand(updateCmd)
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

		// Always update the cache when explicitly running update
		saveUpdateState(&updateState{
			CheckedAt:     time.Now(),
			LatestVersion: latest,
		})

		if latest == Version {
			fmt.Printf("Already up to date (v%s)\n", Version)
			return nil
		}

		// 3. Find the right asset
		archiveName := fmt.Sprintf("plaud-cli_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
		var downloadURL string
		for _, a := range release.Assets {
			if a.Name == archiveName {
				downloadURL = a.BrowserDownloadURL
				break
			}
		}
		if downloadURL == "" {
			return fmt.Errorf("no release asset found for %s/%s", runtime.GOOS, runtime.GOARCH)
		}

		fmt.Printf("Updating v%s → v%s ...\n", Version, latest)

		// 4. Download tarball
		dlResp, err := http.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("downloading release: %w", err)
		}
		defer dlResp.Body.Close()

		if dlResp.StatusCode != http.StatusOK {
			return fmt.Errorf("download returned %d", dlResp.StatusCode)
		}

		// 5. Extract binary from tarball
		gz, err := gzip.NewReader(dlResp.Body)
		if err != nil {
			return fmt.Errorf("decompressing: %w", err)
		}
		defer gz.Close()

		tr := tar.NewReader(gz)
		var newBinary []byte
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("reading tar: %w", err)
			}
			if hdr.Name == "plaud" {
				newBinary, err = io.ReadAll(tr)
				if err != nil {
					return fmt.Errorf("reading binary from tar: %w", err)
				}
				break
			}
		}
		if newBinary == nil {
			return fmt.Errorf("binary not found in archive")
		}

		// 6. Replace self
		execPath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("finding current executable: %w", err)
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			return fmt.Errorf("resolving executable path: %w", err)
		}

		// Write to a temp file first
		tmpFile, err := os.CreateTemp("", "plaud-update-*")
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

		// Try direct rename first (works when same filesystem + writable)
		if err := os.Rename(tmpPath, execPath); err != nil {
			// Fall back to sudo cp for permission-restricted paths like /usr/local/bin
			sudoCmd := exec.Command("sudo", "cp", tmpPath, execPath)
			sudoCmd.Stdin = os.Stdin
			sudoCmd.Stdout = os.Stdout
			sudoCmd.Stderr = os.Stderr
			if sudoErr := sudoCmd.Run(); sudoErr != nil {
				os.Remove(tmpPath)
				return fmt.Errorf("replacing binary (tried sudo): %w", sudoErr)
			}
			os.Remove(tmpPath)
		}

		fmt.Printf("Updated to v%s\n", latest)
		return nil
	},
}
