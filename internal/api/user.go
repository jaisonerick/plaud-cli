package api

import "context"

// GetMe returns the authenticated user's info.
func (c *Client) GetMe(ctx context.Context) (*UserInfo, error) {
	var resp UserResponse
	if err := c.Do(ctx, "GET", "/user/me", nil, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
