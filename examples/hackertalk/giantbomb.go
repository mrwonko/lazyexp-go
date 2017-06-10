package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	marioFranchiseURL = "https://www.giantbomb.com/api/franchise/3025-1/"
	soulsFranchiseURL = "https://www.giantbomb.com/api/franchise/3025-2169/"
)

func giantbombQuery(ctx context.Context, url string, fields []string, result interface{}) error {
	const apiKey = "you wish"
	query := fmt.Sprintf("%s?api_key=%s&format=json&field_list=%s", url, apiKey, strings.Join(fields, ","))

	req, err := http.NewRequest("GET", query, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}

	err = json.NewDecoder(resp.Body).Decode(result)
	if err != nil {
		resp.Body.Close()
		return err
	}
	return resp.Body.Close()
}

var (
	franchiseFields = []string{"deck", "games"}
	gameFields      = []string{"name", "id", "deck", "platforms"}
	platformFields  = []string{"name", "deck", "install_base"}
)

type giantbombFranchise struct {
	Results struct {
		Deck  string
		Games []struct {
			Name string
			ID   int
			URL  string `json:"api_detail_url"`
		}
	}
}

type giantbombGame struct {
	Results struct {
		Name      string
		ID        int
		Deck      string
		Platforms []struct {
			URL  string `json:"api_detail_url"`
			Name string
		}
	}
}

type giantbombPlatform struct {
	Results struct {
		Name        string
		Deck        string
		InstallBase string `json:"install_base"`
	}
}
