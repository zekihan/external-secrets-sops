package http

import (
	"fmt"
	"github.com/zekihan/external-secrets-sops/config"
	"io"
	"net/http"
	"time"
)

func GetURL(client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", config.GetConfig().UserAgent)
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	body, err := getBody(response)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		if response.StatusCode == 409 {
			return nil, fmt.Errorf("failed to get URL with status code: 409\n%s", string(body))
		}
		return nil, fmt.Errorf("failed to get URL with status code: %d", response.StatusCode)
	}
	return body, nil
}

func getBody(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func CreateHTTPClient() *http.Client {
	client := &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
			DisableCompression: true,
		},
	}
	return client
}
