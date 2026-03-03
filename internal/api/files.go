package api

import "context"

// ListRecordings returns all recordings (simplified view).
func (c *Client) ListRecordings(ctx context.Context) ([]RecordingSimple, error) {
	var resp RecordingListResponse
	if err := c.Do(ctx, "GET", "/file/simple/web?skip=0&limit=99999&sort=-created_at", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetDetail returns full recording details including transcript and summary.
func (c *Client) GetDetail(ctx context.Context, id string) (*RecordingDetail, error) {
	var resp RecordingDetailResponse
	if err := c.Do(ctx, "GET", "/file/detail/"+id, nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
