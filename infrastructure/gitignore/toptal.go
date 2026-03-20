package gitignore

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const toptalBaseURL = "https://www.toptal.com/developers/gitignore/api"

// ToptalClient implements domain/gitignore.ContentGenerator using the Toptal gitignore API.
type ToptalClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewToptalClient() *ToptalClient {
	return &ToptalClient{httpClient: &http.Client{}, baseURL: toptalBaseURL}
}

// NewToptalClientWithURL creates a client with a custom base URL (for testing).
func NewToptalClientWithURL(baseURL string) *ToptalClient {
	return &ToptalClient{httpClient: &http.Client{}, baseURL: baseURL}
}

func (c *ToptalClient) Generate(ctx context.Context, technologies []string) (string, error) {
	if len(technologies) == 0 {
		return "", fmt.Errorf("no technologies specified")
	}

	url := c.baseURL + "/" + strings.Join(technologies, ",")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("toptal api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("toptal api returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading response: %w", err)
	}

	return string(body), nil
}
