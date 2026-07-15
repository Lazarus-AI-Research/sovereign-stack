// Package dockerapi is a minimal Docker Engine REST client over the unix
// socket. Deliberately stdlib-only: the proxy's dependency surface is part
// of its security posture.
package dockerapi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const apiVersion = "v1.43"

type Client struct {
	http *http.Client
	base string
}

func New(socketPath string) *Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &Client{
		http: &http.Client{Transport: transport},
		base: "http://docker/" + apiVersion,
	}
}

// NewForTest points the client at an arbitrary HTTP base URL.
func NewForTest(base string, client *http.Client) *Client {
	return &Client{http: client, base: base}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any) (*http.Response, error) {
	endpoint := c.base + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	resp, err := c.do(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker api %s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(payload)))
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ── typed operations ─────────────────────────────────────────────────────

type Container struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/_ping", nil, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping: %s", resp.Status)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/version", nil, nil, &out)
	return out, err
}

// ListProject lists containers belonging to the compose project.
func (c *Client) ListProject(ctx context.Context, project string) ([]Container, error) {
	filters := map[string][]string{
		"label": {"com.docker.compose.project=" + project},
	}
	raw, _ := json.Marshal(filters)
	query := url.Values{"all": {"true"}, "filters": {string(raw)}}
	var out []Container
	err := c.doJSON(ctx, http.MethodGet, "/containers/json", query, nil, &out)
	return out, err
}

// FindService resolves a compose service name to a container within project.
func (c *Client) FindService(ctx context.Context, project, service string) (*Container, error) {
	filters := map[string][]string{
		"label": {
			"com.docker.compose.project=" + project,
			"com.docker.compose.service=" + service,
		},
	}
	raw, _ := json.Marshal(filters)
	query := url.Values{"all": {"true"}, "filters": {string(raw)}}
	var out []Container
	if err := c.doJSON(ctx, http.MethodGet, "/containers/json", query, nil, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no container for service %q in project %q", service, project)
	}
	return &out[0], nil
}

func (c *Client) Inspect(ctx context.Context, id string) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/containers/"+id+"/json", nil, nil, &out)
	return out, err
}

func (c *Client) Restart(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodPost, "/containers/"+id+"/restart", url.Values{"t": {"10"}}, nil, nil)
}

// Logs returns the last `tail` lines, demultiplexing Docker's stream format.
func (c *Client) Logs(ctx context.Context, id string, tail int) (string, error) {
	query := url.Values{
		"stdout": {"true"},
		"stderr": {"true"},
		"tail":   {fmt.Sprintf("%d", tail)},
	}
	resp, err := c.do(ctx, http.MethodGet, "/containers/"+id+"/logs", query, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf("docker logs: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	if resp.Header.Get("Content-Type") == "application/vnd.docker.multiplexed-stream" {
		return DemuxLogs(raw), nil
	}
	// TTY containers stream raw bytes.
	return string(raw), nil
}

// DemuxLogs strips the 8-byte frame headers of Docker's multiplexed stream.
func DemuxLogs(raw []byte) string {
	var out strings.Builder
	for len(raw) >= 8 {
		size := binary.BigEndian.Uint32(raw[4:8])
		frameEnd := 8 + int(size)
		if frameEnd > len(raw) {
			break
		}
		out.Write(raw[8:frameEnd])
		raw = raw[frameEnd:]
	}
	return out.String()
}

// PullImage pulls ref and drains the progress stream.
func (c *Client) PullImage(ctx context.Context, ref string) error {
	name, tag := ref, "latest"
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		name, tag = ref[:i], ref[i+1:]
	}
	query := url.Values{"fromImage": {name}, "tag": {tag}}
	resp, err := c.do(ctx, http.MethodPost, "/images/create", query, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker pull: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	// The stream reports per-layer progress; the last record carries any error.
	decoder := json.NewDecoder(resp.Body)
	for {
		var event struct {
			Error string `json:"error"`
		}
		if err := decoder.Decode(&event); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if event.Error != "" {
			return fmt.Errorf("docker pull: %s", event.Error)
		}
	}
}

func (c *Client) InspectImage(ctx context.Context, ref string) (map[string]any, error) {
	var out map[string]any
	err := c.doJSON(ctx, http.MethodGet, "/images/"+url.PathEscape(ref)+"/json", nil, nil, &out)
	return out, err
}

// ExportImages streams a Docker-compatible image archive for the exact refs.
func (c *Client) ExportImages(ctx context.Context, refs []string) (io.ReadCloser, error) {
	query := url.Values{}
	for _, ref := range refs {
		query.Add("names", ref)
	}
	resp, err := c.do(ctx, http.MethodGet, "/images/get", query, nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("docker image export: %s: %s", resp.Status, strings.TrimSpace(string(payload)))
	}
	return resp.Body, nil
}

// RunJob creates and starts a one-shot container. Everything except env
// values comes from the validated allowlist configuration.
func (c *Client) RunJob(ctx context.Context, image, network string, binds, env []string, cmd []string) (string, error) {
	body := map[string]any{
		"Image": image,
		"Env":   env,
		"Cmd":   cmd,
		"HostConfig": map[string]any{
			"Binds":       binds,
			"NetworkMode": network,
			"AutoRemove":  false,
		},
		"Labels": map[string]string{"com.lazarus.sovereign.job": "evals"},
	}
	var created struct {
		ID string `json:"Id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/create", nil, body, &created); err != nil {
		return "", err
	}
	if err := c.doJSON(ctx, http.MethodPost, "/containers/"+created.ID+"/start", nil, nil, nil); err != nil {
		return created.ID, err
	}
	return created.ID, nil
}
