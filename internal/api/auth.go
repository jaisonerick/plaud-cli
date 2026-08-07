package api

import (
	"context"
	"fmt"
	"net/url"
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

// SecurityConfigResponse is returned by GET /config/security. It is public:
// the key it carries is what passwords are sealed with before they are sent.
type SecurityConfigResponse struct {
	Envelope
	Data struct {
		PassPubKey    string `json:"pass_pub_key"`
		PassAlgorithm string `json:"pass_algorithm"`
	} `json:"data"`
}

// PasswordLoginResponse is returned by POST /auth/access-token.
type PasswordLoginResponse struct {
	Envelope
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// PasswordLogin exchanges an email and password for an access token.
//
// The password never travels in the clear: the server publishes a public key
// and only accepts a sealed envelope (see EncryptPassword). The key is fetched
// per login rather than cached, because a rotation on the server would
// otherwise turn every login into an unexplained "wrong password".
func (c *Client) PasswordLogin(ctx context.Context, email, password string) (string, error) {
	var config SecurityConfigResponse
	if err := c.Do(ctx, "GET", "/config/security", nil, &config); err != nil {
		return "", fmt.Errorf("fetching the server's public key: %w", err)
	}
	if config.Data.PassPubKey == "" {
		return "", fmt.Errorf("the server published no public key to seal the password with")
	}
	if config.Data.PassAlgorithm != "secp256k1" {
		return "", fmt.Errorf("the server expects %q, which this client cannot produce; upgrade plaud",
			config.Data.PassAlgorithm)
	}

	sealed, err := EncryptPassword(config.Data.PassPubKey, password)
	if err != nil {
		return "", err
	}

	var resp PasswordLoginResponse
	err = c.PostForm(ctx, "/auth/access-token", url.Values{
		"username":           {email},
		"password":           {sealed},
		"client_id":          {"web"},
		"password_encrypted": {"true"},
	}, &resp)
	if err != nil {
		return "", err
	}
	if resp.AccessToken == "" {
		return "", fmt.Errorf("login succeeded but returned no access token")
	}
	return resp.AccessToken, nil
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
