package api

import "context"

// ListTags returns all user-defined tags.
func (c *Client) ListTags(ctx context.Context) ([]Tag, error) {
	var resp TagListResponse
	if err := c.Do(ctx, "GET", "/filetag/", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}
