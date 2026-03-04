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

// CreateTag creates a new tag with the given name.
func (c *Client) CreateTag(ctx context.Context, name string) error {
	body := map[string]string{"name": name}
	var resp Envelope
	return c.Do(ctx, "POST", "/filetag/", body, &resp)
}

// DeleteTag deletes a tag by ID.
func (c *Client) DeleteTag(ctx context.Context, id string) error {
	var resp Envelope
	return c.Do(ctx, "DELETE", "/filetag/"+id, nil, &resp)
}
