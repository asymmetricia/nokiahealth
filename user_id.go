package nokiahealth

import (
	"encoding/json"
	"strconv"
)

type UserId string

func (u *UserId) UnmarshalJSON(data []byte) error {
	var i int
	if err := json.Unmarshal(data, &i); err == nil {
		*u = UserId(strconv.Itoa(i))
		return nil
	}

	return json.Unmarshal(data, (*string)(u))
}
