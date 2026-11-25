package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const serviceRoot = "/redfish/v1"

// Resource is a single fetched Redfish resource (or a fragment of one).
type Resource struct {
	Path    string
	Data    map[string]any
	Raw     []byte
	Fetched time.Time
}

type call struct {
	done chan struct{}
	res  *Resource
	err  error
}

// Client talks to a Redfish service. Responses are cached so navigation is instant
// once a resource has been seen.
type Client struct {
	Base     string
	user     string
	pass     string
	http     *http.Client
	AuthMode string // "Session" or "Basic"

	mu         sync.Mutex
	token      string
	sessionURI string
	cache      map[string]*Resource
	inflight   map[string]*call
	sem        chan struct{}

	Requests atomic.Int64
}

// NewClient builds a client for host (IP, hostname, or full URL).
func NewClient(host, user, pass string, insecure bool) (*Client, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("host is required")
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid host %q", host)
	}
	tr := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: insecure},
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 20 * time.Second,
	}
	return &Client{
		Base:     u.Scheme + "://" + u.Host,
		user:     user,
		pass:     pass,
		http:     &http.Client{Transport: tr, Timeout: 90 * time.Second},
		cache:    map[string]*Resource{},
		inflight: map[string]*call{},
		sem:      make(chan struct{}, 4), // BMCs are slow and fragile; keep concurrency low
	}, nil
}

// Connect fetches the service root, then authenticates (session first, basic as fallback)
// and verifies the credentials work.
func (c *Client) Connect(ctx context.Context) error {
	root, err := c.fetch(ctx, serviceRoot, false)
	if err != nil {
		return fmt.Errorf("cannot reach Redfish service: %w", err)
	}

	sessions := serviceRoot + "/SessionService/Sessions"
	if l, ok := dig(root.Data, "Links", "Sessions", "@odata.id").(string); ok && l != "" {
		sessions = NormalizePath(l)
	}
	if err := c.login(ctx, sessions); err == nil {
		c.AuthMode = "Session"
	} else {
		c.AuthMode = "Basic"
	}

	// Verify credentials against something that needs auth.
	probe := serviceRoot + "/Systems"
	if l, ok := dig(root.Data, "Systems", "@odata.id").(string); ok {
		probe = NormalizePath(l)
	}
	if _, err := c.fetch(ctx, probe, true); err != nil {
		return err
	}
	c.mu.Lock()
	c.cache[serviceRoot] = root
	c.mu.Unlock()
	// Refetch the root with auth: some services hide links from anonymous users.
	_, _ = c.Get(ctx, serviceRoot, true)
	return nil
}

func (c *Client) login(ctx context.Context, sessionsPath string) error {
	body, _ := json.Marshal(map[string]string{"UserName": c.user, "Password": c.pass})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+sessionsPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	tok := resp.Header.Get("X-Auth-Token")
	if resp.StatusCode/100 != 2 || tok == "" {
		return fmt.Errorf("session login failed: %s", resp.Status)
	}
	c.mu.Lock()
	c.token = tok
	c.sessionURI = NormalizePath(resp.Header.Get("Location"))
	c.mu.Unlock()
	return nil
}

// Logout deletes the session (if any). Best effort.
func (c *Client) Logout() {
	c.mu.Lock()
	tok, uri := c.token, c.sessionURI
	c.token = ""
	c.mu.Unlock()
	if tok == "" || uri == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.Base+uri, nil)
	if err != nil {
		return
	}
	req.Header.Set("X-Auth-Token", tok)
	if resp, err := c.http.Do(req); err == nil {
		resp.Body.Close()
	}
}

// Cached returns a cached resource without any network access.
func (c *Client) Cached(path string) *Resource {
	path = NormalizePath(path)
	base, frag := splitFragment(path)
	c.mu.Lock()
	r := c.cache[base]
	c.mu.Unlock()
	if r == nil {
		return nil
	}
	if frag != "" {
		fr, err := fragmentOf(r, path, frag)
		if err != nil {
			return nil
		}
		return fr
	}
	return r
}

// CachedAll returns a snapshot of every cached resource.
func (c *Client) CachedAll() map[string]*Resource {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]*Resource, len(c.cache))
	for k, v := range c.cache {
		out[k] = v
	}
	return out
}

// Get returns a resource, from cache unless force is set. Paths with a JSON-pointer
// fragment ("/redfish/v1/Chassis/1/Thermal#/Fans/0") are resolved inside the parent.
func (c *Client) Get(ctx context.Context, path string, force bool) (*Resource, error) {
	path = NormalizePath(path)
	base, frag := splitFragment(path)

	c.mu.Lock()
	if r, ok := c.cache[base]; ok && !force {
		c.mu.Unlock()
		if frag != "" {
			return fragmentOf(r, path, frag)
		}
		return r, nil
	}
	if cl, ok := c.inflight[base]; ok {
		c.mu.Unlock()
		select {
		case <-cl.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if cl.err != nil {
			return nil, cl.err
		}
		if frag != "" {
			return fragmentOf(cl.res, path, frag)
		}
		return cl.res, nil
	}
	cl := &call{done: make(chan struct{})}
	c.inflight[base] = cl
	c.mu.Unlock()

	cl.res, cl.err = c.fetch(ctx, base, true)

	c.mu.Lock()
	if cl.err == nil {
		c.cache[base] = cl.res
	}
	delete(c.inflight, base)
	c.mu.Unlock()
	close(cl.done)

	if cl.err != nil {
		return nil, cl.err
	}
	if frag != "" {
		return fragmentOf(cl.res, path, frag)
	}
	return cl.res, nil
}

// HTTPError is returned for non-2xx responses.
type HTTPError struct {
	Status  int
	Message string
}

func (e *HTTPError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("HTTP %d %s", e.Status, http.StatusText(e.Status))
}

func (c *Client) fetch(ctx context.Context, path string, auth bool) (*Resource, error) {
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.sem }()

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("OData-Version", "4.0")
		if auth {
			c.mu.Lock()
			tok := c.token
			c.mu.Unlock()
			if tok != "" {
				req.Header.Set("X-Auth-Token", tok)
			} else {
				req.SetBasicAuth(c.user, c.pass)
			}
		}
		c.Requests.Add(1)
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}

		if resp.StatusCode == http.StatusUnauthorized && auth && attempt == 0 {
			// Session may have expired: try to log in again once.
			c.mu.Lock()
			hadToken := c.token != ""
			c.mu.Unlock()
			if hadToken && c.login(ctx, sessionsFromURI(c.sessionURI)) == nil {
				continue
			}
		}
		if resp.StatusCode/100 != 2 {
			herr := &HTTPError{Status: resp.StatusCode, Message: extractRedfishError(raw)}
			if resp.StatusCode == http.StatusUnauthorized {
				herr.Message = "authentication failed - check username and password"
			}
			return nil, herr
		}
		var data map[string]any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		if err := dec.Decode(&data); err != nil {
			return nil, fmt.Errorf("invalid JSON from %s: %w", path, err)
		}
		var pretty bytes.Buffer
		if json.Indent(&pretty, raw, "", "  ") == nil {
			raw = pretty.Bytes()
		}
		return &Resource{Path: path, Data: data, Raw: raw, Fetched: time.Now()}, nil
	}
}

func sessionsFromURI(uri string) string {
	if i := strings.LastIndex(uri, "/"); i > 0 {
		return uri[:i]
	}
	return serviceRoot + "/SessionService/Sessions"
}

func extractRedfishError(raw []byte) string {
	var e struct {
		Error struct {
			Message  string `json:"message"`
			Extended []struct {
				Message string `json:"Message"`
			} `json:"@Message.ExtendedInfo"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return ""
	}
	if len(e.Error.Extended) > 0 && e.Error.Extended[0].Message != "" {
		return e.Error.Extended[0].Message
	}
	return e.Error.Message
}

// NormalizePath turns absolute URLs / odd variants into a clean "/redfish/v1/..." path.
func NormalizePath(p string) string {
	p = strings.TrimSpace(p)
	if strings.Contains(p, "://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
			if u.Fragment != "" {
				p += "#" + u.Fragment
			}
		}
	}
	if p == "" {
		return serviceRoot
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	base, frag := splitFragment(p)
	if len(base) > 1 {
		base = strings.TrimRight(base, "/")
	}
	if frag != "" {
		return base + "#" + frag
	}
	return base
}

func splitFragment(p string) (string, string) {
	if i := strings.Index(p, "#"); i >= 0 {
		return p[:i], p[i+1:]
	}
	return p, ""
}

// fragmentOf resolves a JSON pointer such as "/Fans/0" inside r.
func fragmentOf(r *Resource, full, frag string) (*Resource, error) {
	var cur any = r.Data
	for _, part := range strings.Split(strings.Trim(frag, "/"), "/") {
		if part == "" {
			continue
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch v := cur.(type) {
		case map[string]any:
			cur = v[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(v) {
				return nil, fmt.Errorf("fragment %q not found", frag)
			}
			cur = v[i]
		default:
			return nil, fmt.Errorf("fragment %q not found", frag)
		}
	}
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("fragment %q is not an object", frag)
	}
	raw, _ := json.MarshalIndent(m, "", "  ")
	return &Resource{Path: full, Data: m, Raw: raw, Fetched: r.Fetched}, nil
}

// dig walks nested maps by key.
func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}
