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

	body, status, err := c.get(ctx, c.baseURL+"/"+strings.Join(technologies, ","))
	if err != nil {
		return "", fmt.Errorf("toptal api request: %w", err)
	}
	if status == http.StatusOK {
		return body, nil
	}
	if status != http.StatusNotFound {
		return "", fmt.Errorf("toptal api returned status %d", status)
	}

	// 404: one or more identifiers are unknown. Filter against the list endpoint
	// and retry with only the valid subset.
	valid, err := c.filterValid(ctx, technologies)
	if err != nil {
		return "", fmt.Errorf("toptal api returned status 404; could not validate templates: %w", err)
	}
	if len(valid) == 0 {
		return "", fmt.Errorf("toptal api returned status 404; none of %v are valid template identifiers", technologies)
	}

	body, status, err = c.get(ctx, c.baseURL+"/"+strings.Join(valid, ","))
	if err != nil {
		return "", fmt.Errorf("toptal api request: %w", err)
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("toptal api returned status %d", status)
	}
	return body, nil
}

func (c *ToptalClient) get(ctx context.Context, url string) (string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, fmt.Errorf("creating request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}
	return string(body), resp.StatusCode, nil
}

func (c *ToptalClient) filterValid(ctx context.Context, technologies []string) ([]string, error) {
	body, status, err := c.get(ctx, c.baseURL+"/list")
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("list endpoint returned status %d", status)
	}

	known := make(map[string]bool)
	for _, name := range strings.FieldsFunc(body, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	}) {
		known[strings.ToLower(strings.TrimSpace(name))] = true
	}

	var valid []string
	for _, tech := range technologies {
		if known[strings.ToLower(tech)] {
			valid = append(valid, tech)
		}
	}
	return valid, nil
}
