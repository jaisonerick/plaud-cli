package api

import "context"

// UploadInfo sends a telemetry event (required before some downloads).
func (c *Client) UploadInfo(ctx context.Context, event, data string) error {
	req := UploadInfoRequest{Event: event, Data: data}
	return c.Do(ctx, "POST", "/others/upload-info", &req, nil)
}

// GetTempURL returns a presigned S3 URL for the audio file.
func (c *Client) GetTempURL(ctx context.Context, id string) (string, error) {
	var resp TempURLResponse
	if err := c.Do(ctx, "GET", "/file/temp-url/"+id, nil, &resp); err != nil {
		return "", err
	}
	return resp.Data.URL, nil
}

// ExportDocument requests a document export (transcript or summary in various formats).
func (c *Client) ExportDocument(ctx context.Context, fileID, docType, format string) (string, error) {
	req := ExportRequest{
		FileID: fileID,
		Type:   docType,
		Format: format,
	}
	var resp ExportResponse
	if err := c.Do(ctx, "POST", "/file/document/export", &req, &resp); err != nil {
		return "", err
	}
	return resp.Data.URL, nil
}
