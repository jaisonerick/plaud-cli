package api

import (
	"context"
)

// SendCodeRequest is sent to POST /auth/otp-send-code.
type SendCodeRequest struct {
	Username string `json:"username"`
	UserArea string `json:"user_area"`
}

// SendCodeResponse is returned by POST /auth/otp-send-code.
type SendCodeResponse struct {
	Envelope
	Token string `json:"token"`
}

// OTPLoginRequest is sent to POST /auth/otp-login.
type OTPLoginRequest struct {
	Code               string `json:"code"`
	Token              string `json:"token"`
	UserArea           string `json:"user_area"`
	RequireSetPassword bool   `json:"require_set_password"`
}

// OTPLoginResponse is returned by POST /auth/otp-login.
type OTPLoginResponse struct {
	Envelope
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	IsNewUser   bool   `json:"is_new_user"`
}

// SendCode requests a sign-in code to be sent to the given email.
// Returns an OTP token that must be passed to VerifyCode.
func (c *Client) SendCode(ctx context.Context, email string) (string, error) {
	req := SendCodeRequest{
		Username: email,
		UserArea: "BR",
	}

	var resp SendCodeResponse
	if err := c.Do(ctx, "POST", "/auth/otp-send-code", req, &resp); err != nil {
		return "", err
	}

	return resp.Token, nil
}

// VerifyCode exchanges the OTP token + code for an access token.
func (c *Client) VerifyCode(ctx context.Context, otpToken, code string) (string, error) {
	req := OTPLoginRequest{
		Code:               code,
		Token:              otpToken,
		UserArea:           "BR",
		RequireSetPassword: true,
	}

	var resp OTPLoginResponse
	if err := c.Do(ctx, "POST", "/auth/otp-login", req, &resp); err != nil {
		return "", err
	}

	return resp.AccessToken, nil
}
