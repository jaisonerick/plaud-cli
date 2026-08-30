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

// PasswordLogin exchanges an email and password for an access token.
//
// The password never travels in the clear: the server publishes a public key
// and only accepts a sealed envelope (see EncryptPassword). The key is fetched
// per login rather than cached, because a rotation on the server would
// otherwise turn every login into an unexplained "wrong password".
func (c *Client) PasswordLogin(ctx context.Context, email, password string) (*Session, error) {
	var config SecurityConfigResponse
	if err := c.Do(ctx, "GET", "/config/security", nil, &config); err != nil {
		return nil, fmt.Errorf("fetching the server's public key: %w", err)
	}
	if config.Data.PassPubKey == "" {
		return nil, fmt.Errorf("the server published no public key to seal the password with")
	}
	if config.Data.PassAlgorithm != "secp256k1" {
		return nil, fmt.Errorf("the server expects %q, which this client cannot produce; upgrade plaud",
			config.Data.PassAlgorithm)
	}

	sealed, err := EncryptPassword(config.Data.PassPubKey, password)
	if err != nil {
		return nil, err
	}

	var resp SessionResponse
	err = c.PostForm(ctx, "/auth/access-token", url.Values{
		"username":           {email},
		"password":           {sealed},
		"client_id":          {"web"},
		"password_encrypted": {"true"},
	}, &resp)
	if err != nil {
		return nil, err
	}
	return c.established(resp)
}

// VerifyCode exchanges the OTP token + code for a session.
func (c *Client) VerifyCode(ctx context.Context, otpToken, code string) (*Session, error) {
	req := OTPLoginRequest{
		Code:               code,
		Token:              otpToken,
		UserArea:           "BR",
		RequireSetPassword: true,
	}

	var resp SessionResponse
	if err := c.Do(ctx, "POST", "/auth/otp-login", req, &resp); err != nil {
		return nil, err
	}
	return c.established(resp)
}

// established reports what a login actually produced.
//
// The check is worth making at every door: a v3 server answers "success" with
// every token field empty, so a client reading the body alone stores nothing
// and says it worked. What makes the session is the cookies, which do() has
// already folded in by the time this runs.
func (c *Client) established(resp SessionResponse) (*Session, error) {
	if c.Session == nil {
		c.Session = &Session{}
	}
	c.Session.take(resp)

	if !c.Session.Valid() {
		return nil, fmt.Errorf("the server accepted the login but handed over nothing to authenticate with "+
			"(version_tag=%q, token_id=%q, no session cookie): this client is too old for the scheme it answered with",
			resp.VersionTag, resp.TokenID)
	}
	return c.Session, nil
}
