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
	RefreshToken string
	token        *oauth2.Token
	HTTPClient   *http.Client
}

// NewUserFromRefreshToken generates a new user that the refresh token is for.
// At time of first use, a new access token will be retrieved.
func (c *Client) NewUserFromRefreshToken(ctx context.Context, refreshToken string) (*User, error) {
	u := &User{
		Client:       c,
		RefreshToken: refreshToken,
	}
	u.HTTPClient = &http.Client{Transport: u}

	var err error
	u.token, err = u.TokenContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("creating user from refresh token: %w", err)
	}

	return u, nil
}

func (u *User) Token() (*oauth2.Token, error) {
	return u.TokenContext(context.Background())
}

func (u *User) TokenContext(ctx context.Context) (*oauth2.Token, error) {
	form := url.Values{}
	form.Set("action", "requesttoken")
	form.Set("client_id", u.OAuth2Config.ClientID)
	form.Set("client_secret", u.OAuth2Config.ClientSecret)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", u.RefreshToken)
	body := bytes.NewBufferString(form.Encode())

	req, err := http.NewRequest("POST", "https://wbsapi.withings.net/v2/oauth2", body)
	if err != nil {
		return nil, fmt.Errorf("producing new request: %w", err)
	}

	req.Header.Set("content-type", "application/x-www-form-urlencoded")

	res, err := (*WithingsRoundTripper)(http.DefaultClient).RoundTrip(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, _ := ioutil.ReadAll(res.Body)
		return nil, fmt.Errorf("non-2XX %d from server: %q", res.StatusCode, string(body))
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
		return nil, fmt.Errorf("reading body: %w", err)
	}

	if err := json.Unmarshal(resBody, &response); err != nil {
		return nil, fmt.Errorf("decoding body: %w", err)
	}

	u.RefreshToken = response.RefreshToken

	return &oauth2.Token{
		AccessToken:  response.AccessToken,
		TokenType:    response.TokenType,
		RefreshToken: response.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}, nil
}

func (u *User) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	return oauth2.NewClient(
		req.Context(),
		oauth2.ReuseTokenSource(u.token, u)).
		Transport.
		RoundTrip(req)
}

var _ oauth2.TokenSource = (*User)(nil)
