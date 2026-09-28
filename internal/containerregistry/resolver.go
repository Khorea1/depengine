// Package containerregistry resolves mutable container tags to immutable OCI
// digests without pulling image content into the local engine store.
package containerregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/containerref"
	"github.com/Khorea1/depengine/internal/run"
)

const (
	manifestAccept = "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json"
	maxManifest    = 16 << 20
	maxTokenReply  = 1 << 20
)

// Credentials carries one ephemeral registry username/password pair. Values
// are used only in Authorization headers and are never embedded in URLs.
type Credentials struct {
	Username string
	Secret   string
}

// ResolveTagDigest resolves source:tag through the OCI Distribution API and
// returns the immutable sha256 digest advertised by the registry. When a
// registry omits Docker-Content-Digest, the digest is computed over the exact
// manifest bytes returned by GET.
func ResolveTagDigest(ctx context.Context, source, tag string, credentials *Credentials) (string, error) {
	if err := containerref.ValidateRepository(source); err != nil {
		return "", fmt.Errorf("container registry: %w", err)
	}
	if tag == "" {
		tag = "latest"
	}
	if err := containerref.ValidateTag(tag); err != nil {
		return "", fmt.Errorf("container registry: %w", err)
	}
	if credentials != nil {
		if credentials.Username == "" || credentials.Secret == "" || strings.ContainsRune(credentials.Username, ':') {
			return "", errors.New("container registry: invalid credentials")
		}
	}

	registry, repository := splitRepository(source)
	scheme := "https"
	if loopbackRegistry(registry) {
		scheme = "http"
	}
	manifestURL := (&url.URL{
		Scheme: scheme,
		Host:   registry,
		Path:   "/v2/" + repository + "/manifests/" + tag,
	}).String()

	client := newHTTPClient()
	digest, needGet, err := resolveManifestRequest(ctx, client, http.MethodHead, manifestURL, repository, credentials)
	if err != nil {
		return "", err
	}
	if !needGet {
		return digest, nil
	}
	return resolveManifestBody(ctx, client, manifestURL, repository, credentials)
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("container registry: stopped after 10 redirects")
			}
			if len(via) == 0 {
				return nil
			}
			previous := via[len(via)-1]
			if strings.EqualFold(previous.URL.Scheme, "https") && !strings.EqualFold(req.URL.Scheme, "https") {
				return errors.New("container registry: refusing HTTPS redirect to an insecure endpoint")
			}
			if !strings.EqualFold(previous.URL.Host, req.URL.Host) {
				req.Header.Del("Authorization")
			}
			return nil
		},
	}
}

func splitRepository(source string) (registry, repository string) {
	first, rest, hasSlash := strings.Cut(source, "/")
	if hasSlash && (strings.ContainsAny(first, ".:") || strings.EqualFold(first, "localhost")) {
		registry := first
		if strings.EqualFold(registry, "docker.io") || strings.EqualFold(registry, "index.docker.io") {
			registry = "registry-1.docker.io"
		}
		return registry, rest
	}
	if !hasSlash {
		return "registry-1.docker.io", "library/" + source
	}
	return "registry-1.docker.io", source
}

func loopbackRegistry(registry string) bool {
	host := registry
	if parsedHost, _, err := net.SplitHostPort(registry); err == nil {
		host = parsedHost
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func resolveManifestRequest(ctx context.Context, client *http.Client, method, manifestURL, repository string, credentials *Credentials) (digest string, needGet bool, err error) {
	resp, err := doManifestRequest(ctx, client, method, manifestURL, repository, credentials)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusMethodNotAllowed && method == http.MethodHead {
		return "", true, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", false, fmt.Errorf("container registry: manifest %s: %s", strings.ToLower(method), resp.Status)
	}
	digest = strings.ToLower(strings.TrimSpace(resp.Header.Get("Docker-Content-Digest")))
	if digest == "" {
		return "", true, nil
	}
	if err := containerref.ValidateDigest(digest); err != nil {
		return "", false, fmt.Errorf("container registry: invalid manifest digest: %w", err)
	}
	return digest, false, nil
}

func resolveManifestBody(ctx context.Context, client *http.Client, manifestURL, repository string, credentials *Credentials) (string, error) {
	resp, err := doManifestRequest(ctx, client, http.MethodGet, manifestURL, repository, credentials)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("container registry: manifest get: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxManifest+1))
	if err != nil {
		return "", fmt.Errorf("container registry: read manifest: %w", run.RedactError(err))
	}
	if len(body) == 0 {
		return "", errors.New("container registry: empty manifest response")
	}
	if len(body) > maxManifest {
		return "", fmt.Errorf("container registry: manifest exceeds %d bytes", maxManifest)
	}
	sum := sha256.Sum256(body)
	computed := "sha256:" + hex.EncodeToString(sum[:])
	advertised := strings.ToLower(strings.TrimSpace(resp.Header.Get("Docker-Content-Digest")))
	if advertised == "" {
		return computed, nil
	}
	if err := containerref.ValidateDigest(advertised); err != nil {
		return "", fmt.Errorf("container registry: invalid manifest digest: %w", err)
	}
	if advertised != computed {
		return "", errors.New("container registry: advertised manifest digest does not match response body")
	}
	return advertised, nil
}

func doManifestRequest(ctx context.Context, client *http.Client, method, manifestURL, repository string, credentials *Credentials) (*http.Response, error) {
	resp, err := sendManifestRequest(ctx, client, method, manifestURL, "", credentials, false)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	_ = resp.Body.Close()
	scheme, params, err := parseChallenge(challenge)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(scheme) {
	case "bearer":
		token, err := fetchBearerToken(ctx, client, params, repository, credentials)
		if err != nil {
			return nil, err
		}
		return sendManifestRequest(ctx, client, method, manifestURL, token, nil, false)
	case "basic":
		if credentials == nil {
			return nil, errors.New("container registry: authentication required")
		}
		return sendManifestRequest(ctx, client, method, manifestURL, "", credentials, true)
	default:
		return nil, fmt.Errorf("container registry: unsupported authentication challenge %q", scheme)
	}
}

func sendManifestRequest(ctx context.Context, client *http.Client, method, rawURL, bearer string, credentials *Credentials, basic bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("container registry: request: %w", run.RedactError(err))
	}
	req.Header.Set("Accept", manifestAccept)
	req.Header.Set("User-Agent", "github.com/Khorea1/depengine/0.1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	} else if basic && credentials != nil {
		req.SetBasicAuth(credentials.Username, credentials.Secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("container registry: request: %w", run.RedactError(err))
	}
	return resp, nil
}

func parseChallenge(header string) (string, map[string]string, error) {
	header = strings.TrimSpace(header)
	if header == "" {
		return "", nil, errors.New("container registry: authentication required without a challenge")
	}
	scheme, raw, found := strings.Cut(header, " ")
	if !found || strings.TrimSpace(raw) == "" {
		return "", nil, fmt.Errorf("container registry: invalid authentication challenge %q", run.RedactSensitiveText(header))
	}
	params := make(map[string]string)
	for len(raw) > 0 {
		raw = strings.TrimSpace(raw)
		key, rest, ok := strings.Cut(raw, "=")
		if !ok {
			return "", nil, errors.New("container registry: invalid authentication challenge parameters")
		}
		key = strings.TrimSpace(key)
		rest = strings.TrimSpace(rest)
		if key == "" || !strings.HasPrefix(rest, "\"") {
			return "", nil, errors.New("container registry: invalid authentication challenge parameters")
		}
		rest = rest[1:]
		end := strings.IndexByte(rest, '"')
		if end < 0 {
			return "", nil, errors.New("container registry: invalid authentication challenge parameters")
		}
		params[strings.ToLower(key)] = rest[:end]
		raw = strings.TrimSpace(rest[end+1:])
		if raw == "" {
			break
		}
		if !strings.HasPrefix(raw, ",") {
			return "", nil, errors.New("container registry: invalid authentication challenge parameters")
		}
		raw = raw[1:]
	}
	return scheme, params, nil
}

func fetchBearerToken(ctx context.Context, client *http.Client, params map[string]string, repository string, credentials *Credentials) (string, error) {
	realm := params["realm"]
	if realm == "" {
		return "", errors.New("container registry: bearer challenge is missing realm")
	}
	u, err := url.Parse(realm)
	if err != nil || u.Host == "" {
		return "", errors.New("container registry: bearer challenge has invalid realm")
	}
	secureTokenEndpoint := u.Scheme == "https" || (u.Scheme == "http" && loopbackRegistry(u.Host))
	if !secureTokenEndpoint {
		return "", errors.New("container registry: refusing bearer token exchange over an insecure endpoint")
	}
	q := u.Query()
	if service := params["service"]; service != "" {
		q.Set("service", service)
	}
	scope := params["scope"]
	if scope == "" {
		scope = "repository:" + repository + ":pull"
	}
	q.Set("scope", scope)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", fmt.Errorf("container registry: token request: %w", run.RedactError(err))
	}
	req.Header.Set("User-Agent", "github.com/Khorea1/depengine/0.1")
	if credentials != nil {
		req.SetBasicAuth(credentials.Username, credentials.Secret)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("container registry: token request: %w", run.RedactError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("container registry: token request: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenReply+1))
	if err != nil {
		return "", fmt.Errorf("container registry: read token response: %w", run.RedactError(err))
	}
	if len(body) > maxTokenReply {
		return "", errors.New("container registry: token response is too large")
	}
	var reply struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &reply); err != nil {
		return "", errors.New("container registry: invalid token response")
	}
	token := reply.Token
	if token == "" {
		token = reply.AccessToken
	}
	if token == "" {
		return "", errors.New("container registry: token response is missing token")
	}
	return token, nil
}
