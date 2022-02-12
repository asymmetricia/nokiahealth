package withings

import (
	"golang.org/x/oauth2"
)

// Endpoint is Withing's OAuth 2.0 endpoint.
var Oauth2Endpoint = oauth2.Endpoint{
	AuthURL:  "https://account.withings.com/oauth2_user/authorize2",
	TokenURL: "https://wbsapi.withings.net/v2/oauth2",
}
