package cmd

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

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

func init() {
	rootCmd.AddCommand(updateCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update plaud to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Fetch latest release
		resp, err := http.Get("https://api.github.com/repos/jaisonerick/plaud-cli/releases/latest")
		if err != nil {
			return fmt.Errorf("fetching latest release: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
		}

		var release ghRelease
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return fmt.Errorf("parsing release: %w", err)
		}

		// 2. Compare versions
		latest := strings.TrimPrefix(release.TagName, "v")
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

		tmpPath := execPath + ".new"
		if err := os.WriteFile(tmpPath, newBinary, 0755); err != nil {
			return fmt.Errorf("writing new binary: %w", err)
		}

		if err := os.Rename(tmpPath, execPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("replacing binary: %w", err)
		}

		fmt.Printf("Updated to v%s\n", latest)
		return nil
	},
}
