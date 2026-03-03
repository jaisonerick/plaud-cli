package api

import (
	"context"
	"net/url"
)

// Login authenticates with email and password, returning the access token.
func (c *Client) Login(ctx context.Context, username, password string) (string, error) {
	values := url.Values{
		"username":  {username},
		"password":  {password},
		"client_id": {"web"},
	}

	var resp AuthResponse
	if err := c.PostForm(ctx, "/auth/access-token", values, &resp); err != nil {
		return "", err
	}

	return resp.Data.AccessToken, nil
}
