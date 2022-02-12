package nokiahealth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/asymmetricia/nokiahealth/enum/status"
	"golang.org/x/oauth2"
)

const (
	apiHost                       = "wbsapi.withings.net"
	getIntradayActivitiesURL      = "https://" + apiHost + "/v2/measure"
	getActivityMeasuresURL        = "https://" + apiHost + "/v2/measure"
	getWorkoutsURL                = "https://" + apiHost + "/v2/measure"
	getBodyMeasureURL             = "https://" + apiHost + "/measure"
	getSleepMeasureURL            = "https://" + apiHost + "/v2/sleep"
	getSleepSummaryURL            = "https://" + apiHost + "/v2/sleep"
	createNotficationURL          = "https://" + apiHost + "/notify"
	listNotificationsURL          = "https://" + apiHost + "/notify"
	getNotificationInformationURL = "https://" + apiHost + "/notify"
	revokeNotificationURL         = "https://" + apiHost + "/notify"
)

// Scope defines the types of scopes accepted by the API.
type Scope string

const (
	// ScopeUserMetrics provides access to the Getmeas actions.
	ScopeUserMetrics Scope = "user.metrics"
	// ScopeUserInfo provides access to the user information.
	ScopeUserInfo Scope = "user.info"
	// ScopeUserActivity provides access to the users activity data.
	ScopeUserActivity Scope = "user.activity"
)

// Rand provides a function type to allow passing in custom random functions
// used for state generation.
type Rand func() (string, error)

// generateRandomString generates a new random string using crytpo/rand. The
// result is base64 encoded for use in URLs.
func generateRandomString() (string, error) {
	buf := make([]byte, 64)
	_, err := rand.Read(buf)
	return base64.URLEncoding.EncodeToString(buf), err
}

// Client contains all the required information to interact with the Withings API.
type Client struct {
	OAuth2Config    *oauth2.Config
	SaveRawResponse bool
	IncludePath     bool
	Rand            Rand
	Timeout         time.Duration
}

// NewClient creates a new client using the Ouath2 information provided. The
// required parameters can be obtained when developers register with Withings
// to use the API.
func NewClient(clientID string, clientSecret string, redirectURL string) Client {
	return Client{
		OAuth2Config: &oauth2.Config{
			RedirectURL:  redirectURL,
			ClientID:     clientID,
			ClientSecret: clientSecret,
			// Scopes:       []string{"user.metrics", "user.activity"},
			Scopes:   []string{"user.activity,user.metrics,user.info"},
			Endpoint: Oauth2Endpoint,
		},
		Rand:    generateRandomString,
		Timeout: 5 * time.Second,
	}
}

// SetScope allows for setting the scope of the client which is used during
// authorization requests for new users. By default the scope will be all
// scopes. This is also not thread safe.
func (c *Client) SetScope(scopes ...string) {
	c.OAuth2Config.Scopes = []string{strings.Join([]string(scopes), ",")}
}

// getContext returns a context set to time out after the duration specified
// in the client.
func (c *Client) getContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), c.Timeout)
}

// AuthCodeURL generates the URL user authorization URL. Users should be redirected
// to this URL so they can allow your application. They will then be directed back
// to the redirectURL provided when the client was created. This redirection
// will contain the authentication code needed to generate an access token.
//
// The state parameter of the request is generated using crypto/rand
// and returned as state. The random generation function can be replaced
// by assigning a new function to Client.Rand.
func (c *Client) AuthCodeURL() (url string, state string, err error) {
	state, err = c.Rand()
	return c.OAuth2Config.AuthCodeURL(state), state, err
}

// GenerateAccessToken generates the access token from the authorization code. The
// authorization code is the one provided in the parameters of the redirect request
// from the URL generated by AuthCodeURL. Generally this isn't directly called and
// create user is used instead. The state is also not validated and is left for the
// calling methods.
func (c *Client) GenerateAccessToken(ctx context.Context, code string) (*oauth2.Token, error) {
	form := url.Values{}
	form.Set("action", "requesttoken")
	form.Set("client_id", c.OAuth2Config.ClientID)
	form.Set("client_secret", c.OAuth2Config.ClientSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", c.OAuth2Config.RedirectURL)
	body := bytes.NewBufferString(form.Encode())

	req, err := http.NewRequest("POST", "https://wbsapi.withings.net/v2/oauth2", body)
	if err != nil {
		return nil, fmt.Errorf("producing new request: %w", err)
	}

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

	return &oauth2.Token{
		AccessToken:  response.AccessToken,
		TokenType:    response.TokenType,
		RefreshToken: response.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(response.ExpiresIn) * time.Second),
	}, nil
}

// WithingsRoundTripper unwraps withings responses so the oauth2 library can
// function happily.
type WithingsRoundTripper http.Client

func (w *WithingsRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	res, err := (*http.Client)(w).Do(request)
	if err != nil {
		return nil, err
	}

	if res.StatusCode != 200 {
		return res, nil
	}

	defer res.Body.Close()

	var response struct {
		Status int
		Body   json.RawMessage
	}

	resBody, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	if err := json.Unmarshal(resBody, &response); err != nil {
		return nil, fmt.Errorf("error decoding response %q: %w", string(resBody), err)
	}

	if response.Status != 0 {
		return nil, fmt.Errorf(
			"bad status code %d in body, see "+
				"https://developer.withings.com/api-reference/#section/Response-status"+
				" -- full body was: %q",
			response.Status,
			string(resBody),
		)
	}

	res.Body = ioutil.NopCloser(bytes.NewBuffer(response.Body))
	return res, nil
}

var _ http.RoundTripper = (*WithingsRoundTripper)(nil)

// NewUserFromAuthCode generates a new user by requesting the token using the
// authentication code provided. This is generally only used after a user
// has just authorized access and the client is processing the redirect.
func (c *Client) NewUserFromAuthCode(ctx context.Context, code string) (*User, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: (*WithingsRoundTripper)(http.DefaultClient),
		Timeout:   c.Timeout,
	})

	t, err := c.GenerateAccessToken(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain token: %s", err)
	}

	u := &User{
		Client:       c,
		RefreshToken: t.RefreshToken,
		token:        t,
	}

	u.HTTPClient = &http.Client{Transport: u}
	return u, nil
}

// GetIntradayActivity is the same as GetIntraDayActivityCtx but doesn't require a context to be provided.
func (u *User) GetIntradayActivity(params *IntradayActivityQueryParam) (IntradayActivityResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetIntradayActivityCtx(ctx, params)
}

// GetIntradayActivityCtx retreieves intraday activites from the API. Special permissions provided by
// Withings Health are required to use this resource.
func (u *User) GetIntradayActivityCtx(ctx context.Context, params *IntradayActivityQueryParam) (IntradayActivityResp, error) {
	intraDayActivityResponse := IntradayActivityResp{}

	// Building query params
	v := url.Values{}
	v.Add("action", "getintradayactivity")

	if params != nil {
		if params.StartDate != nil {
			v.Add(GetFieldName(*params, "StartDate"), strconv.FormatInt(params.StartDate.Unix(), 10))
		}
		if params.EndDate != nil {
			v.Add(GetFieldName(*params, "EndDate"), strconv.FormatInt(params.EndDate.Unix(), 10))
		}
	}

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getIntradayActivitiesURL, v.Encode())
	if u.Client.IncludePath {
		intraDayActivityResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return intraDayActivityResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return intraDayActivityResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return intraDayActivityResponse, err
	}
	if u.Client.SaveRawResponse {
		intraDayActivityResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &intraDayActivityResponse)
	if err != nil {
		return intraDayActivityResponse, err
	}
	if intraDayActivityResponse.Status != status.OperationWasSuccessful {
		return intraDayActivityResponse, fmt.Errorf("api returned an error: %s", intraDayActivityResponse.Error)
	}

	return intraDayActivityResponse, nil
}

// GetActivityMeasures is the same as GetActivityMeasuresCtx but doesn't require a context to be provided.
func (u *User) GetActivityMeasures(params *ActivityMeasuresQueryParam) (ActivitiesMeasuresResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetActivityMeasuresCtx(ctx, params)
}

// GetActivityMeasuresCtx retrieves the activity measurements as specified by the config
// provided. If the start time is missing the current time minus one day will be used.
// If the end time is missing the current day will be used.
func (u *User) GetActivityMeasuresCtx(ctx context.Context, params *ActivityMeasuresQueryParam) (ActivitiesMeasuresResp, error) {
	activityMeasureResponse := ActivitiesMeasuresResp{}

	// Building the query params
	v := url.Values{}
	v.Add("action", "getactivity")

	if params != nil {
		// if params.Date != nil {
		// 	v.Add(GetFieldName(*params, "Date"), params.Date.Format("2006-01-02"))
		// }
		if params.StartDateYMD != nil {
			v.Add(GetFieldName(*params, "StartDateYMD"), params.StartDateYMD.Format("2006-01-02"))
		} else {
			v.Add(GetFieldName(*params, "StartDateYMD"), time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
		}
		if params.EndDateYMD != nil {
			v.Add(GetFieldName(*params, "EndDateYMD"), params.EndDateYMD.Format("2006-01-02"))
		} else {
			v.Add(GetFieldName(*params, "EndDateYMD"), time.Now().Format("2006-01-02"))
		}
		if params.LasteUpdate != nil {
			v.Add(GetFieldName(*params, "LasteUpdate"), strconv.FormatInt(params.LasteUpdate.Unix(), 10))
		}
	} else {
		params = &ActivityMeasuresQueryParam{}
		v.Add(GetFieldName(*params, "StartDateYMD"), time.Now().AddDate(0, 0, -1).Format("2006-01-02"))
		v.Add(GetFieldName(*params, "EndDateYMD"), time.Now().Format("2006-01-02"))

	}

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getActivityMeasuresURL, v.Encode())
	if u.Client.IncludePath {
		activityMeasureResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return activityMeasureResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return activityMeasureResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return activityMeasureResponse, err
	}
	if u.Client.SaveRawResponse {
		activityMeasureResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &activityMeasureResponse)
	if err != nil {
		return activityMeasureResponse, err
	}

	if activityMeasureResponse.Status != status.OperationWasSuccessful {
		return activityMeasureResponse, fmt.Errorf("api returned an error: %s", activityMeasureResponse.Error)
	}

	// Parse date time if possible.
	if activityMeasureResponse.Body.Date != nil && activityMeasureResponse.Body.TimeZone != nil {
		location, err := time.LoadLocation(*activityMeasureResponse.Body.TimeZone)
		if err != nil {
			return activityMeasureResponse, err
		}

		t, err := time.Parse("2006-01-02", *activityMeasureResponse.Body.Date)
		if err != nil {
			return activityMeasureResponse, err
		}

		t = t.In(location)
		activityMeasureResponse.Body.ParsedDate = &t

		activityMeasureResponse.Body.SingleValue = true
	}

	for aID := range activityMeasureResponse.Body.Activities {
		location, err := time.LoadLocation(activityMeasureResponse.Body.Activities[aID].TimeZone)
		if err != nil {
			return activityMeasureResponse, err
		}

		t, err := time.Parse("2006-01-02", activityMeasureResponse.Body.Activities[aID].Date)
		if err != nil {
			return activityMeasureResponse, err
		}

		t = t.In(location)
		activityMeasureResponse.Body.Activities[aID].ParsedDate = &t
	}

	return activityMeasureResponse, nil
}

// GetWorkouts is the same as GetWorkoutsCTX but doesn't require a context to be provided.
func (u *User) GetWorkouts(params *WorkoutsQueryParam) (WorkoutResponse, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetWorkoutsCtx(ctx, params)
}

// GetWorkoutsCtx retrieves all the workouts for a given date range based on the values
// provided by params.
func (u *User) GetWorkoutsCtx(ctx context.Context, params *WorkoutsQueryParam) (WorkoutResponse, error) {

	workoutResponse := WorkoutResponse{}

	// Building query params
	v := url.Values{}
	v.Add("action", "getworkouts")

	if params != nil {
		if params.StartDateYMD != nil {
			v.Add(GetFieldName(*params, "StartDateYMD"), params.StartDateYMD.Format("2006-01-02"))
		}
		if params.EndDateYMD != nil {
			v.Add(GetFieldName(*params, "EndDateYMD"), params.EndDateYMD.Format("2006-01-02"))
		}
	}

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getWorkoutsURL, v.Encode())
	if u.Client.IncludePath {
		workoutResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return workoutResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return workoutResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return workoutResponse, nil
	}
	if u.Client.SaveRawResponse {
		workoutResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &workoutResponse)
	if err != nil {
		return workoutResponse, err
	}
	if workoutResponse.Status != status.OperationWasSuccessful {
		return workoutResponse, fmt.Errorf("api returned an error: %s", workoutResponse.Error)
	}

	// Parse dates if possible
	if workoutResponse.Body != nil {
		for i := range workoutResponse.Body.Series {
			d := time.Unix(workoutResponse.Body.Series[i].StartDate, 0)
			workoutResponse.Body.Series[i].StartDateParsed = &d

			d = time.Unix(workoutResponse.Body.Series[i].EndDate, 0)
			workoutResponse.Body.Series[i].EndDateParsed = &d

			location, err := time.LoadLocation(workoutResponse.Body.Series[i].TimeZone)
			if err != nil {
				return workoutResponse, err
			}

			t, err := time.Parse("2006-01-02", workoutResponse.Body.Series[i].Date)
			if err != nil {
				return workoutResponse, err
			}

			t = t.In(location)

			workoutResponse.Body.Series[i].DateParsed = &t
		}
	}

	return workoutResponse, nil

}

// GetBodyMeasures is the same as GetBodyMeasuresCtx but doesn't require a context to be provided.
func (u *User) GetBodyMeasures(params *BodyMeasuresQueryParams) (BodyMeasuresResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetBodyMeasuresCtx(ctx, params)
}

// GetBodyMeasuresCtx retrieves the body measurements as specified by the config
// provided.
func (u *User) GetBodyMeasuresCtx(ctx context.Context, params *BodyMeasuresQueryParams) (BodyMeasuresResp, error) {
	bodyMeasureResponse := BodyMeasuresResp{}

	// Building query params
	v := url.Values{}
	v.Add("action", "getmeas")

	if params != nil {
		if params.StartDate != nil {
			v.Add(GetFieldName(*params, "StartDate"), strconv.FormatInt(params.StartDate.Unix(), 10))
		}
		if params.EndDate != nil {
			v.Add(GetFieldName(*params, "EndDate"), strconv.FormatInt(params.EndDate.Unix(), 10))
		}
		if params.LastUpdate != nil {
			v.Add(GetFieldName(*params, "LastUpdate"), strconv.FormatInt(params.EndDate.Unix(), 10))
		}
		if params.DevType != nil {
			v.Add(GetFieldName(*params, "DevType"), strconv.Itoa(int(*params.DevType)))
		}
		if params.MeasType != nil {
			v.Add(GetFieldName(*params, "MeasType"), strconv.Itoa(int(*params.MeasType)))
		}
		if params.Category != nil {
			v.Add(GetFieldName(*params, "Category"), strconv.Itoa(*params.Category))
		}
		if params.Limit != nil {
			v.Add(GetFieldName(*params, "Limit"), strconv.Itoa(*params.Limit))
		}
		if params.Offset != nil {
			v.Add(GetFieldName(*params, "Offset"), strconv.Itoa(*params.Offset))
		}
	}

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getBodyMeasureURL, v.Encode())
	if u.Client.IncludePath {
		bodyMeasureResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return bodyMeasureResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return bodyMeasureResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return bodyMeasureResponse, err
	}
	if u.Client.SaveRawResponse {
		bodyMeasureResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &bodyMeasureResponse)
	if err != nil {
		return bodyMeasureResponse, err
	}
	if bodyMeasureResponse.Status != status.OperationWasSuccessful {
		return bodyMeasureResponse, fmt.Errorf("api returned an error: %s", bodyMeasureResponse.Error)
	}

	if params != nil && params.ParseResponse {
		bodyMeasureResponse.ParsedResponse = bodyMeasureResponse.ParseData()
	}

	return bodyMeasureResponse, nil

}

// GetSleepMeasures is the same as GetSleepMeasuresCtx but doesn't require a context to be provided.
func (u *User) GetSleepMeasures(params *SleepMeasuresQueryParam) (SleepMeasuresResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetSleepMeasuresCtx(ctx, params)
}

// GetSleepMeasuresCtx retrieves the sleep measurements as specified by the config
// provided. Start and end dates are requires so if the param is not provided
// one is generated for the past 24 hour timeframe.
func (u *User) GetSleepMeasuresCtx(ctx context.Context, params *SleepMeasuresQueryParam) (SleepMeasuresResp, error) {
	sleepMeasureRepsonse := SleepMeasuresResp{}

	// Building query params
	v := url.Values{}
	v.Add("action", "get")

	// Params are required for this api call. To be consident we handle empty params and build
	// one with sensible defaults if needed.
	if params == nil {
		params = &SleepMeasuresQueryParam{}
		params.StartDate = time.Now()
		params.EndDate = time.Now().AddDate(0, 0, -1)
	}

	v.Add(GetFieldName(*params, "StartDate"), strconv.FormatInt(params.StartDate.Unix(), 10))
	v.Add(GetFieldName(*params, "EndDate"), strconv.FormatInt(params.EndDate.Unix(), 10))

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getSleepMeasureURL, v.Encode())
	if u.Client.IncludePath {
		sleepMeasureRepsonse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return sleepMeasureRepsonse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return sleepMeasureRepsonse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return sleepMeasureRepsonse, err
	}
	if u.Client.SaveRawResponse {
		sleepMeasureRepsonse.RawResponse = body
	}

	err = json.Unmarshal(body, &sleepMeasureRepsonse)
	if err != nil {
		return sleepMeasureRepsonse, err
	}
	if sleepMeasureRepsonse.Status != status.OperationWasSuccessful {
		return sleepMeasureRepsonse, fmt.Errorf("api returned an error: %s", sleepMeasureRepsonse.Error)
	}

	// Parse dates
	if sleepMeasureRepsonse.Body != nil {
		for i := range sleepMeasureRepsonse.Body.Series {
			t := time.Unix(sleepMeasureRepsonse.Body.Series[i].StartDate, 0)
			sleepMeasureRepsonse.Body.Series[i].StartDateParsed = &t

			t = time.Unix(sleepMeasureRepsonse.Body.Series[i].EndDate, 0)
			sleepMeasureRepsonse.Body.Series[i].EndDateParsed = &t
		}
	}

	return sleepMeasureRepsonse, nil
}

// GetSleepSummary is the same as GetSleepSummaryCtx but doesn't require a context to be provided.
func (u *User) GetSleepSummary(params *SleepSummaryQueryParam) (SleepSummaryResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetSleepSummaryCtx(ctx, params)
}

// GetSleepSummaryCtx retrieves the sleep summary information provided. A SleepSummaryQueryParam is
// required as a timeframe is needed by the API. If null is provided the last 24 hours will be used.
func (u *User) GetSleepSummaryCtx(ctx context.Context, params *SleepSummaryQueryParam) (SleepSummaryResp, error) {
	sleepSummaryResponse := SleepSummaryResp{}

	// Building query params
	v := url.Values{}
	v.Add("action", "getsummary")

	// Params are required for this api call. To be consident we handle empty params and build
	// one with sensible defaults if needed.
	if params == nil {
		params = &SleepSummaryQueryParam{}
		t1 := time.Now()
		t2 := time.Now().AddDate(0, 0, -1)
		params.StartDateYMD = &t1
		params.EndDateYMD = &t2
	}

	// Although the API currently says the type is a UNIX time stamp the reality is it's a date string.
	v.Add(GetFieldName(*params, "StartDateYMD"), params.StartDateYMD.Format("2006-01-02"))
	v.Add(GetFieldName(*params, "EndDateYMD"), params.EndDateYMD.Format("2006-01-02"))

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", getSleepSummaryURL, v.Encode())
	if u.Client.IncludePath {
		sleepSummaryResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return sleepSummaryResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return sleepSummaryResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return sleepSummaryResponse, err
	}
	if u.Client.SaveRawResponse {
		sleepSummaryResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &sleepSummaryResponse)
	if err != nil {
		return sleepSummaryResponse, err
	}
	if sleepSummaryResponse.Status != status.OperationWasSuccessful {
		return sleepSummaryResponse, fmt.Errorf("api returned an error: %s", sleepSummaryResponse.Error)
	}

	// Parse all the date fields.
	if sleepSummaryResponse.Body != nil {
		for i := range sleepSummaryResponse.Body.Series {

			// Parse the normal UNIX time stamps.
			startDate := time.Unix(sleepSummaryResponse.Body.Series[i].StartDate, 0)
			endDate := time.Unix(sleepSummaryResponse.Body.Series[i].EndDate, 0)
			sleepSummaryResponse.Body.Series[i].StartDateParsed = &startDate
			sleepSummaryResponse.Body.Series[i].EndDateParsed = &endDate

			// Parse the goofy YYYY-MM-DD plus location date.
			location, err := time.LoadLocation(sleepSummaryResponse.Body.Series[i].TimeZone)
			if err != nil {
				return sleepSummaryResponse, err
			}

			t, err := time.Parse("2006-01-02", sleepSummaryResponse.Body.Series[i].Date)
			if err != nil {
				return sleepSummaryResponse, err
			}

			t = t.In(location)
			sleepSummaryResponse.Body.Series[i].DateParsed = &t
		}
	}

	return sleepSummaryResponse, nil

}

// CreateNotification is the same as CreateNotificationCtx but doesn't require a context to be provided.
func (u *User) CreateNotification(params *CreateNotificationParam) (CreateNotificationResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.CreateNotificationCtx(ctx, params)
}

// CreateNotificationCtx creates a new notification.
func (u *User) CreateNotificationCtx(ctx context.Context, params *CreateNotificationParam) (CreateNotificationResp, error) {
	createNotificationResponse := CreateNotificationResp{}

	// Build a params if nil as it is required.
	if params == nil {
		params = &CreateNotificationParam{}
	}

	// Building query params.
	v := url.Values{}
	v.Add("action", "subscribe")

	v.Add(GetFieldName(*params, "CallbackURL"), params.CallbackURL.String())
	v.Add(GetFieldName(*params, "Comment"), params.Comment)
	v.Add(GetFieldName(*params, "Appli"), strconv.Itoa(params.Appli))

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", createNotficationURL, v.Encode())
	if u.Client.IncludePath {
		createNotificationResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return createNotificationResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return createNotificationResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return createNotificationResponse, err
	}
	if u.Client.SaveRawResponse {
		createNotificationResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &createNotificationResponse)
	if err != nil {
		return createNotificationResponse, err
	}
	if createNotificationResponse.Status != status.OperationWasSuccessful {
		return createNotificationResponse, fmt.Errorf("api returned an error: %s", createNotificationResponse.Error)
	}

	return createNotificationResponse, nil
}

// ListNotifications is the same as ListNotificationsCtx but doesn't require a context to be provided.
func (u *User) ListNotifications(params *ListNotificationsParam) (ListNotificationsResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.ListNotificationsCtx(ctx, params)
}

// ListNotificationsCtx lists all the notifications found for the user.
func (u *User) ListNotificationsCtx(ctx context.Context, params *ListNotificationsParam) (ListNotificationsResp, error) {
	listNotificationResponse := ListNotificationsResp{}

	// Building query params.
	v := url.Values{}
	v.Add("action", "list")

	if params != nil {
		if params.Appli != nil {
			v.Add(GetFieldName(*params, "Appli"), strconv.Itoa(*params.Appli))
		}
	}

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", listNotificationsURL, v.Encode())
	if u.Client.IncludePath {
		listNotificationResponse.Path = path
	}
	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return listNotificationResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return listNotificationResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return listNotificationResponse, err
	}
	if u.Client.SaveRawResponse {
		listNotificationResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &listNotificationResponse)
	if err != nil {
		return listNotificationResponse, err
	}
	if listNotificationResponse.Status != status.OperationWasSuccessful {
		return listNotificationResponse, fmt.Errorf("api returned error: %s", listNotificationResponse.Error)
	}

	// Parse dates
	if listNotificationResponse.Body != nil {
		for i := range listNotificationResponse.Body.Profiles {
			d := time.Unix(listNotificationResponse.Body.Profiles[0].Expires, 0)
			listNotificationResponse.Body.Profiles[i].ExpiresParsed = &d
		}
	}

	return listNotificationResponse, nil
}

// GetNotificationInformation is the same as GetNotificationInformationCtx but doesn't require a context to be provided.
func (u *User) GetNotificationInformation(params *NotificationInfoParam) (NotificationInfoResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.GetNotificationInformationCtx(ctx, params)
}

// GetNotificationInformationCtx lists all the notifications found for the user.
func (u *User) GetNotificationInformationCtx(ctx context.Context, params *NotificationInfoParam) (NotificationInfoResp, error) {
	notificationInfoResponse := NotificationInfoResp{}

	// Building query params.
	v := url.Values{}
	v.Add("action", "get")

	if params == nil {
		params = &NotificationInfoParam{}
	}

	v.Add(GetFieldName(*params, "CallbackURL"), params.CallbackURL.String())
	v.Add(GetFieldName(*params, "Appli"), strconv.Itoa(*params.Appli))

	// Sending reqeust to the API.
	path := fmt.Sprintf("%s?%s", getNotificationInformationURL, v.Encode())
	if u.Client.IncludePath {
		notificationInfoResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return notificationInfoResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return notificationInfoResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return notificationInfoResponse, err
	}
	if u.Client.SaveRawResponse {
		notificationInfoResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &notificationInfoResponse)
	if err != nil {
		return notificationInfoResponse, err
	}
	if notificationInfoResponse.Status != status.OperationWasSuccessful {
		return notificationInfoResponse, fmt.Errorf("api returned an error: %s", notificationInfoResponse.Error)
	}

	// Parse dates
	if notificationInfoResponse.Body != nil {
		d := time.Unix(notificationInfoResponse.Body.Expires, 0)
		notificationInfoResponse.Body.ExpiresParsed = &d

	}

	return notificationInfoResponse, nil
}

// RevokeNotification is the same as RevokeNotificationCtx but doesn't require a context to be provided.
func (u *User) RevokeNotification(params *RevokeNotificationParam) (RevokeNotificationResp, error) {
	ctx, cancel := u.Client.getContext()
	defer cancel()
	return u.RevokeNotificationCtx(ctx, params)
}

// RevokeNotificationCtx revokes a notification so it no longer sends.
func (u *User) RevokeNotificationCtx(ctx context.Context, params *RevokeNotificationParam) (RevokeNotificationResp, error) {
	revokeResponse := RevokeNotificationResp{}

	// Building query params.
	v := url.Values{}
	v.Add("action", "revoke")

	if params == nil {
		params = &RevokeNotificationParam{}
	}

	v.Add(GetFieldName(*params, "CallbackURL"), params.CallbackURL.String())
	v.Add(GetFieldName(*params, "Appli"), strconv.Itoa(*params.Appli))

	// Sending request to the API.
	path := fmt.Sprintf("%s?%s", revokeNotificationURL, v.Encode())
	if u.Client.IncludePath {
		revokeResponse.Path = path
	}

	req, err := http.NewRequest("GET", path, nil)
	req = req.WithContext(ctx)
	if err != nil {
		return revokeResponse, fmt.Errorf("failed to build request: %s", err)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return revokeResponse, err
	}
	defer resp.Body.Close()

	// Processing API response.
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return revokeResponse, err
	}
	if u.Client.SaveRawResponse {
		revokeResponse.RawResponse = body
	}

	err = json.Unmarshal(body, &revokeResponse)
	if err != nil {
		return revokeResponse, err
	}
	if revokeResponse.Status != status.OperationWasSuccessful {
		return revokeResponse, fmt.Errorf("api returned an error: %s", revokeResponse.Error)
	}

	return revokeResponse, nil

}
