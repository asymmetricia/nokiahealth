package withings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// User is a Withings Health user account that can be interacted with via the
// api. A user object should not be copied.
type User struct {
	*Client
	OauthToken *oauth2.Token
	HTTPClient *http.Client
}

// NewUserFromAccessToken returns a user with the given access token. If it's
// expired, refreshToken is used to retrieve a refresh token. The resulting user
// may have a different refresh token, and this should be checked and recorded if
// it changes.
func (c *Client) NewUserFromAccessToken(ctx context.Context, accessToken string, tokenExpiry time.Time, refreshToken string) (*User, error) {
	u := &User{
		Client: c,
		OauthToken: &oauth2.Token{
			RefreshToken: refreshToken,
			AccessToken:  accessToken,
			Expiry:       tokenExpiry,
		},
	}

	u.HTTPClient = &http.Client{Transport: u}

	if u.OauthToken.Expiry.After(time.Now()) {
		return c.NewUserFromRefreshToken(ctx, refreshToken)
	}

	return u, nil
}

// NewUserFromRefreshToken generates a new user that the refresh token is for. At
// time of first use, a new access token will be retrieved. The user may end up
// with a different refresh token, and this should be checked and recorded if it
// changes.
func (c *Client) NewUserFromRefreshToken(ctx context.Context, refreshToken string) (*User, error) {
	u := &User{
		Client: c,
		OauthToken: &oauth2.Token{
			RefreshToken: refreshToken,
		},
	}
	u.HTTPClient = &http.Client{Transport: u}

	var err error
	_, err = u.TokenContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating user from refresh token: %w", err)
	}

	return u, nil
}

// Token returns the user's oauth token, refreshing it if necessary. After a call
// to Token, the OauthToken field may have changed, and RefreshToken should
// persisted if so.
func (u *User) Token() (*oauth2.Token, error) {
	return u.TokenContext(context.Background())
}

// TokenContext is as per Token, above, but accepts a context, which will be used
// for API calls if necessary.
func (u *User) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	if u.OauthToken.Expiry.After(time.Now()) {
		return u.OauthToken, nil
	}

	// Refresh the token
	form := url.Values{}
	form.Set("action", "requesttoken")
	form.Set("client_id", u.OAuth2Config.ClientID)
	form.Set("client_secret", u.OAuth2Config.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", u.OauthToken.RefreshToken)
	body := bytes.NewBufferString(form.Encode())

	req, err := http.NewRequest("POST", "https://wbsapi.withings.net/v2/oauth2", body)
	if err != nil {
		return nil, fmt.Errorf("producing new request in TokenContext: %w", err)
	}

	req.Header.Set("content-type", "application/x-www-form-urlencoded")

	res, err := (*WithingsRoundTripper)(http.DefaultClient).RoundTrip(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("sending request in TokenContext: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, _ := ioutil.ReadAll(res.Body)
		return nil, fmt.Errorf("non-2XX %d from server in TokenContext: %q", res.StatusCode, string(body))
	}

	var response struct {
		UserId       UserId `json:"userid"`
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		CsrfToken    string `json:"csrf_token"`
		TokenType    string `json:"token_type"`
	}

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body in TokenContext: %w", err)
	}

	if err := json.Unmarshal(resBody, &response); err != nil {
		return nil, fmt.Errorf("decoding body in TokenContext: %w", err)
	}

	u.OauthToken = &oauth2.Token{
		AccessToken:  response.AccessToken,
		TokenType:    response.TokenType,
		RefreshToken: response.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}
	return u.OauthToken, nil
}

func (u *User) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	return oauth2.NewClient(req.Context(), u).
		Transport.
		RoundTrip(req)
}

var _ oauth2.TokenSource = (*User)(nil)
