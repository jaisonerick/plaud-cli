package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// GetTempURL returns a presigned S3 URL for the audio file.
func (c *Client) GetTempURL(ctx context.Context, id string) (string, error) {
	var resp TempURLResponse
	if err := c.Do(ctx, "GET", "/file/temp-url/"+id, nil, &resp); err != nil {
		return "", err
	}
	return resp.URL, nil
}

// DownloadGzipped fetches a gzipped presigned URL and writes the decompressed content to disk.
// Handles the case where the HTTP client transparently decompresses the response.
func (c *Client) DownloadGzipped(ctx context.Context, fileURL, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}

	// Disable automatic decompression so we can handle gzip ourselves
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("downloading: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	// Try gzip decompression; if it fails, assume content is already decompressed
	var content []byte
	gr, err := gzip.NewReader(bytes.NewReader(body))
	if err == nil {
		content, err = io.ReadAll(gr)
		gr.Close()
		if err != nil {
			return fmt.Errorf("decompressing: %w", err)
		}
	} else {
		content = body
	}

	if err := os.WriteFile(destPath, content, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}
