package filter

import "testing"

func TestIsFiltered_lockFiles(t *testing.T) {
	lockFiles := []string{
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"go.sum",
		"Gemfile.lock",
	}
	for _, name := range lockFiles {
		if !IsFiltered(name) {
			t.Errorf("expected %q to be filtered", name)
		}
	}
}

func TestIsFiltered_binaryFiles(t *testing.T) {
	binaryFiles := []string{
		"image.png",
		"photo.jpg",
		"anim.gif",
		"favicon.ico",
		"document.pdf",
		"font.woff",
		"font.woff2",
		"font.ttf",
		"font.eot",
	}
	for _, name := range binaryFiles {
		if !IsFiltered(name) {
			t.Errorf("expected %q to be filtered", name)
		}
	}
}

func TestIsFiltered_notFiltered(t *testing.T) {
	sourceFiles := []string{
		"main.go",
		"src/app.ts",
		"README.md",
		"config.yaml",
	}
	for _, name := range sourceFiles {
		if IsFiltered(name) {
			t.Errorf("expected %q to not be filtered", name)
		}
	}
}
