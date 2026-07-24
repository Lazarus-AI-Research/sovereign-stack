// Package updates checks the public release feed. Release-feed metadata is
// informational; installation still verifies the release archive with Cosign.
package updates

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Checker struct {
	URL  string
	http *http.Client
}

type Info struct {
	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	Available      bool   `json:"available"`
	ReleaseURL     string `json:"release_url,omitempty"`
	CheckedAt      string `json:"checked_at"`
	CheckError     string `json:"check_error,omitempty"`
}

func New(url string) *Checker {
	return &Checker{URL: url, http: &http.Client{Timeout: 8 * time.Second}}
}

var releaseVersion = regexp.MustCompile(`^v?([0-9]+)\.([0-9]+)\.([0-9]+)(.*)$`)

func newer(candidate, current string) bool {
	a, b := releaseVersion.FindStringSubmatch(candidate), releaseVersion.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false
	}
	for index := 1; index <= 3; index++ {
		left, _ := strconv.Atoi(a[index])
		right, _ := strconv.Atoi(b[index])
		if left != right {
			return left > right
		}
	}
	// A stable release is newer than a prerelease of the same numeric version.
	if b[4] != "" && a[4] == "" {
		return true
	}
	if a[4] == "" || b[4] == "" {
		return false
	}
	left := strings.FieldsFunc(strings.TrimLeft(a[4], "-+"), func(r rune) bool { return r == '.' || r == '-' })
	right := strings.FieldsFunc(strings.TrimLeft(b[4], "-+"), func(r rune) bool { return r == '.' || r == '-' })
	for index := 0; index < len(left) && index < len(right); index++ {
		if left[index] == right[index] {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(left[index])
		rightNumber, rightErr := strconv.Atoi(right[index])
		if leftErr == nil && rightErr == nil {
			return leftNumber > rightNumber
		}
		return left[index] > right[index]
	}
	return len(left) > len(right)
}

func (c *Checker) Check(ctx context.Context, current string) Info {
	info := Info{CurrentVersion: current, CheckedAt: time.Now().UTC().Format(time.RFC3339)}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		info.CheckError = err.Error()
		return info
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "SovereignStack-Control/"+current)
	response, err := c.http.Do(req)
	if err != nil {
		info.CheckError = "Release check unavailable"
		return info
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		info.CheckError = fmt.Sprintf("Release check returned %s", response.Status)
		return info
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if json.NewDecoder(response.Body).Decode(&release) != nil || !releaseVersion.MatchString(release.TagName) {
		info.CheckError = "Release feed returned invalid metadata"
		return info
	}
	info.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	info.ReleaseURL = release.HTMLURL
	info.Available = newer(info.LatestVersion, current)
	return info
}
