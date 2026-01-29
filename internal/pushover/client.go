package pushover

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type Client struct {
	Token string
	User  string
}

func NewClient(token, user string) *Client {
	return &Client{
		Token: token,
		User:  user,
	}
}

func (c *Client) SendMessage(title, message string) error {
	apiUrl := "https://api.pushover.net/1/messages.json"

	params := url.Values{}
	params.Set("token", c.Token)
	params.Set("user", c.User)
	params.Set("title", title)
	params.Set("message", message)
	params.Set("html", "1")

	resp, err := http.PostForm(apiUrl, params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pushover api error: status %s, body %s", resp.Status, string(body))
	}

	return nil
}
