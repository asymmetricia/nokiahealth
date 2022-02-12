package withings

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/asymmetricia/withings/enum/status"
	"github.com/stretchr/testify/require"
)

type TestConfig struct {
	ClientID       string
	ConsumerSecret string
	RefreshToken   string
}

var testClient Client
var testUser *User

func LoadConfig(t *testing.T) {
	rawConfigData, err := ioutil.ReadFile("./test.toml")
	require.NoError(t, err)

	_, err = toml.Decode(string(rawConfigData), &tc)
	require.NoError(t, err)

	require.NotEmpty(t, tc.ClientID, "ClientID in test.toml is required- go "+
		"register a new app pointed to http://localhost:8888")
	require.NotEmpty(t, tc.ConsumerSecret, "ConsumerSecret in test.toml is "+
		"required- go register a new app pointed to http://localhost:8888")

	testClient = NewClient(tc.ClientID, tc.ConsumerSecret, "http://localhost:8888")

	if testUser == nil && tc.RefreshToken != "" {
		testUser, err = testClient.NewUserFromRefreshToken(context.Background(), tc.RefreshToken)
		require.NoError(t, err, "try again after clearing refresh token from test.toml")
	}

	if testUser == nil {
		url, state, err := testClient.AuthCodeURL()
		require.NoError(t, err)
		require.NoError(t, exec.Command("xdg-open", url).Start())

		mux := http.NewServeMux()
		sv := &http.Server{
			Addr:    "localhost:8888",
			Handler: mux,
		}

		mux.Handle("/", http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			defer sv.Shutdown(context.Background())
			require.NoError(t, req.ParseForm())

			require.Equal(t, state, req.Form.Get("state"))

			testUser, err = testClient.NewUserFromAuthCode(context.Background(), req.Form.Get("code"))
			require.NoError(t, err)

			rw.Header().Set("content-type", "text/plain")
			rw.WriteHeader(http.StatusOK)
			fmt.Fprintln(rw, "ok! close this window.")
			if rw, ok := rw.(http.Flusher); ok {
				rw.Flush()
			}
		}))

		if err := sv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			require.NoError(t, err)
		}
	}

	tc.RefreshToken = testUser.RefreshToken
	f, err := os.OpenFile("./test.toml", os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	enc := toml.NewEncoder(f)
	require.NoError(t, enc.Encode(tc))
	require.NoError(t, f.Sync())
	require.NoError(t, f.Close())
}

var tc TestConfig

func TestGetBodyMeasures(t *testing.T) {
	LoadConfig(t)

	startDate := time.Now().AddDate(0, 0, -2)
	endDate := time.Now()

	p := BodyMeasuresQueryParams{
		StartDate: &startDate,
		EndDate:   &endDate,
	}

	m, err := testUser.GetBodyMeasures(&p)
	require.NoError(t, err)

	require.Equal(t, m.Status, status.Status(0), m.Status.String())
}

func TestGetActivityMeasures(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.GetActivityMeasures(nil)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v\n%s\n%s", err, m.RawResponse, m.Path)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", m.Status, m.Status.String())
	}
}

func TestGetIntradayActivity(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.GetIntradayActivity(nil)
	if err != nil {
		t.Fatalf("failed to get intra day measures: %v", err)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get intra day measures with api error %d => %v: %v", m.Status, m.Status.String(), m.Error)
	}
}

func TestGetWorkouts(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.GetWorkouts(nil)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", m.Status, m.Status.String())
	}
}

func TestGetSleepMeasures(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.GetSleepMeasures(nil)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", m.Status, m.Status.String())
	}
}

func TestGetSleepSummary(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.GetSleepSummary(nil)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", m.Status, m.Status.String())
	}
}

func TestNotificationFunctions(t *testing.T) {
	LoadConfig(t)

	// Test creating a notification
	var ul *url.URL
	ul, err := url.Parse("http://example.com")
	p := CreateNotificationParam{
		CallbackURL: *ul,
		Comment:     "this is a test",
		Appli:       1,
	}
	n, err := testUser.CreateNotification(&p)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if n.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", n.Status, n.Status.String())
	}

	// Test finding the notification.
	appli := 1
	p2 := NotificationInfoParam{
		CallbackURL: *ul,
		Appli:       &appli,
	}

	gn, err := testUser.GetNotificationInformation(&p2)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if gn.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", gn.Status, gn.Status.String())
	}

	// Test revoking the notification
	p3 := RevokeNotificationParam{
		CallbackURL: *ul,
		Appli:       &appli,
	}
	rn, err := testUser.RevokeNotification(&p3)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if rn.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", rn.Status, rn.Status.String())
	}
}

func TestListNotifications(t *testing.T) {
	LoadConfig(t)

	m, err := testUser.ListNotifications(nil)
	if err != nil {
		t.Fatalf("failed to get body measurements: %v", err)
	}

	if m.Status != 0 {
		t.Fatalf("failed to get body measurements with api error %d => %v", m.Status, m.Status.String())
	}
}
