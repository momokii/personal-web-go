package medium

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Medium struct {
	Title       string   `json:"title"`
	Date        string   `json:"date"`
	Link        string   `json:"link"`
	Image       string   `json:"image"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type MediumResponse struct {
	DataMedium []Medium `json:"dataMedium"`
}

const (
	BASE_MEDIUM_URL = "https://mediumpostsapi.vercel.app/api/"
)

func GetMediumProfileData(username string) (MediumResponse, error) {

	var mediumData MediumResponse

	if username == "" {
		return MediumResponse{}, fmt.Errorf("username is required")
	}

	MEDIUM_URL := BASE_MEDIUM_URL + username

	// req to url
	req, err := http.NewRequest(http.MethodGet, MEDIUM_URL, nil)
	if err != nil {
		return MediumResponse{}, err
	}

	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return MediumResponse{}, err
	}

	// make sure close the body
	defer func() {
		if res.StatusCode != http.StatusOK {
			io.ReadAll(res.Body)
		}

		res.Body.Close()
	}()

	// error handling
	if res.StatusCode != http.StatusOK {
		return MediumResponse{}, fmt.Errorf("error: %s", res.Status)
	}

	// decode response
	if err := json.NewDecoder(res.Body).Decode(&mediumData); err != nil {
		return MediumResponse{}, err
	}

	return mediumData, nil
}
