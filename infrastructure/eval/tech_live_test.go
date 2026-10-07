//go:build jevlive

// Live measurement of the technology classifier.
//
// The ground truth is already on disk: a generated .gitignore carries the line
// "# Created by https://www.toptal.com/developers/gitignore/api/<identifiers>",
// which is the technology list the classifier has to reproduce. That makes this
// seam measurable against what git-agent itself decided when the file was
// written, with no new labelling.
//
//	TYPESAFE_API_KEY=... go test -tags jevlive -v -run TestLiveTechClassifier ./infrastructure/eval/
package eval_test

import (
	"context"
	"os"
	"strings"
	"testing"

	domainGitignore "github.com/gitagenthq/git-agent/domain/gitignore"
	"github.com/gitagenthq/git-agent/infrastructure/typesafe"
)

func TestLiveTechClassifier(t *testing.T) {
	if os.Getenv("TYPESAFE_API_KEY") == "" {
		t.Skip("TYPESAFE_API_KEY is not set")
	}
	repos := abRepos()
	if len(repos) == 1 && os.Getenv("AB_REPO") == "" {
		repos = defaultMeasuredRepos()
	}

	classifier := typesafe.NewTechClassifier(liveClient(t))
	totalWant, totalHit := 0, 0
	reported := 0

	for _, repo := range repos {
		want, ok := recordedTechnologies(repo)
		if !ok {
			t.Logf("%s: no recorded technology list, skipped", repoLabel(repo))
			continue
		}
		dirs, files, err := repoLayoutOf(repo)
		if err != nil {
			t.Logf("%s: %v, skipped", repoLabel(repo), err)
			continue
		}
		verdict, err := classifier.ClassifyTechnologies(context.Background(),
			domainGitignore.ClassifyRequest{OS: hostOS(), Dirs: dirs, Files: files})
		if err != nil {
			t.Errorf("%s: classification failed: %v", repoLabel(repo), err)
			continue
		}

		got := set(verdict.Technologies)
		hits, missing, extra := 0, []string{}, []string{}
		for _, id := range want {
			if got[id] {
				hits++
				continue
			}
			missing = append(missing, id)
		}
		for _, id := range verdict.Technologies {
			if !set(want)[id] {
				extra = append(extra, id)
			}
		}
		reported++
		totalWant += len(want)
		totalHit += hits
		t.Logf("%-22s recorded %-40s judged %-40s hits %d/%d confidence %.2f",
			repoLabel(repo), strings.Join(want, ","), strings.Join(verdict.Technologies, ","),
			hits, len(want), verdict.Confidence)
		if len(missing) > 0 {
			t.Logf("%-22s missing: %s", "", strings.Join(missing, ","))
		}
		if len(extra) > 0 {
			t.Logf("%-22s extra  : %s", "", strings.Join(extra, ","))
		}
	}

	if reported == 0 {
		t.Skip("no repository carried a recorded technology list")
	}
	t.Logf("\ntechnology agreement with the recorded .gitignore: %d/%d", totalHit, totalWant)
}

// recordedTechnologies reads the technology list a generated .gitignore records.
func recordedTechnologies(repo string) ([]string, bool) {
	raw, err := os.ReadFile(repo + "/.gitignore")
	if err != nil {
		return nil, false
	}
	for _, line := range strings.SplitN(string(raw), "\n", 400) {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "# Created by") {
			continue
		}
		idx := strings.LastIndex(line, "/api/")
		if idx < 0 {
			return nil, false
		}
		var out []string
		for _, id := range strings.Split(line[idx+len("/api/"):], ",") {
			if id = strings.TrimSpace(id); id != "" {
				out = append(out, id)
			}
		}
		return out, len(out) > 0
	}
	return nil, false
}

func set(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[strings.ToLower(strings.TrimSpace(v))] = true
	}
	return out
}

func hostOS() string {
	switch os.Getenv("AB_OS") {
	case "macos", "linux", "windows":
		return os.Getenv("AB_OS")
	default:
		return "macos"
	}
}

// defaultMeasuredRepos are repositories that keep a generated .gitignore with a
// recorded technology list.
func defaultMeasuredRepos() []string {
	out := []string{}
	for _, candidate := range []string{
		"/Users/FradSer/Developer/FradSer/git-agent/git-agent-cli",
		"/Users/FradSer/Developer/FradSer/git-agent/git-agent-home",
		"/Users/FradSer/Developer/FradSer/git-agent/git-agent-proxy",
		"/Users/FradSer/Developer/FradSer/campbase",
		"/Users/FradSer/Developer/FradSer/mai",
	} {
		if _, err := os.Stat(candidate + "/.gitignore"); err == nil {
			out = append(out, candidate)
		}
	}
	return out
}
