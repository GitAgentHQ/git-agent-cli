package gitignore_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	infraGitignore "github.com/fradser/git-agent/infrastructure/gitignore"
)

func TestToptalClient_Generate_Success(t *testing.T) {
	body := "# Created by https://www.toptal.com/developers/gitignore\n*.o\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	client := infraGitignore.NewToptalClientWithURL(srv.URL)
	result, err := client.Generate(context.Background(), []string{"go", "macos"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != body {
		t.Errorf("expected %q, got %q", body, result)
	}
}

func TestToptalClient_Generate_NoTechnologies(t *testing.T) {
	client := infraGitignore.NewToptalClient()
	_, err := client.Generate(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for empty technologies")
	}
}

func TestToptalClient_Generate_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := infraGitignore.NewToptalClientWithURL(srv.URL)
	_, err := client.Generate(context.Background(), []string{"go"})
	if err == nil {
		t.Fatal("expected error for server error")
	}
}
