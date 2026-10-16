package main

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

type AuthConfig struct {
	Issuer      string 
	Resource    string 
	Path        string 
	AllowedSubs []string
}

func authConfigFromEnv() (AuthConfig, error) {
	domain := strings.TrimSuffix(strings.TrimPrefix(os.Getenv("AUTHKIT_DOMAIN"), "https://"), "/")
	resource := strings.TrimSuffix(os.Getenv("MCP_RESOURCE"), "/")
	if domain == "" || resource == "" {
		return AuthConfig{}, errors.New("AUTHKIT_DOMAIN and MCP_RESOURCE must be set for --http mode")
	}
	u, err := url.Parse(resource)
	if err != nil || u.Scheme != "https" || u.Path == "" {
		return AuthConfig{}, fmt.Errorf("MCP_RESOURCE must be a full https URL with a path, like https://x.duckdns.org/mcp (got %q)", resource)
	}

	var subs []string
	for _, s := range strings.Split(os.Getenv("ALLOWED_SUBS"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			subs = append(subs, s)
		}
	}
	if len(subs) == 0 {
		log.Println("auth: ALLOWED_SUBS is empty, so every login will be rejected (rejected user IDs are logged)")
	}
	return AuthConfig{Issuer: "https://" + domain, Resource: resource, Path: u.Path, AllowedSubs: subs}, nil
}

// protectedHandler: metadata is public, the MCP endpoint requires a valid token.
func protectedHandler(mcpHandler http.Handler, cfg AuthConfig, client *http.Client) http.Handler {
	u, _ := url.Parse(cfg.Resource)
	metaPath := "/.well-known/oauth-protected-resource"
	metaURL := u.Scheme + "://" + u.Host + metaPath

	meta := auth.ProtectedResourceMetadataHandler(&oauthex.ProtectedResourceMetadata{
		Resource:               cfg.Resource,
		AuthorizationServers:   []string{cfg.Issuer},
		BearerMethodsSupported: []string{"header"},
	})

	mux := http.NewServeMux()
	mux.Handle(metaPath, meta)
	mux.Handle(metaPath+cfg.Path, meta) // path-suffixed variant some clients try
	mux.Handle(cfg.Path, auth.RequireBearerToken(newVerifier(cfg, client), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: metaURL, // sent in the 401 so Claude can find WorkOS
		ClockSkew:           30 * time.Second,
	})(mcpHandler))
	return mux
}

// newVerifier: signature, issuer, audience, user. The SDK checks expiry.
func newVerifier(cfg AuthConfig, client *http.Client) auth.TokenVerifier {
	keys := &jwks{url: cfg.Issuer + "/oauth2/jwks", client: client}

	return func(ctx context.Context, token string, _ *http.Request) (*auth.TokenInfo, error) {
		c, err := verifyJWT(token, keys)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
		}
		if c.Iss != cfg.Issuer {
			return nil, fmt.Errorf("%w: wrong issuer", auth.ErrInvalidToken)
		}
		if !slices.Contains(c.Aud, cfg.Resource) {
			return nil, fmt.Errorf("%w: wrong audience %v", auth.ErrInvalidToken, c.Aud)
		}
		if !slices.Contains(cfg.AllowedSubs, c.Sub) {
			log.Printf("auth: rejected user %s (add it to ALLOWED_SUBS to allow)", c.Sub)
			return nil, fmt.Errorf("%w: user not allowed", auth.ErrInvalidToken)
		}
		return &auth.TokenInfo{UserID: c.Sub, Expiration: time.Unix(c.Exp, 0), Scopes: strings.Fields(c.Scope)}, nil
	}
}

// ---- JWT (RS256 only) ----

type claims struct {
	Iss   string   `json:"iss"`
	Sub   string   `json:"sub"`
	Aud   audience `json:"aud"`
	Exp   int64    `json:"exp"`
	Nbf   int64    `json:"nbf"`
	Scope string   `json:"scope"`
}

// audience accepts "aud": "x" or "aud": ["x", "y"].
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func verifyJWT(token string, keys *jwks) (*claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("malformed token")
	}

	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := decodeSegment(parts[0], &header); err != nil {
		return nil, fmt.Errorf("bad header: %v", err)
	}
	if header.Alg != "RS256" { 
		return nil, fmt.Errorf("unsupported alg %q", header.Alg)
	}

	key, err := keys.key(header.Kid)
	if err != nil {
		return nil, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, errors.New("bad signature encoding")
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return nil, errors.New("bad signature")
	}

	var c claims
	if err := decodeSegment(parts[1], &c); err != nil {
		return nil, fmt.Errorf("bad claims: %v", err)
	}
	if c.Nbf != 0 && time.Now().Add(30*time.Second).Unix() < c.Nbf {
		return nil, errors.New("token not valid yet")
	}
	return &c, nil
}

func decodeSegment(seg string, out any) error {
	b, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// jwks caches WorkOS's public keys; refetches on unknown key ID, max once a minute.
type jwks struct {
	url     string
	client  *http.Client
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

func (j *jwks) key(kid string) (*rsa.PublicKey, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if k, ok := j.keys[kid]; ok {
		return k, nil
	}
	if time.Since(j.fetched) > time.Minute {
		if err := j.refresh(); err != nil {
			return nil, fmt.Errorf("fetching JWKS: %v", err)
		}
		if k, ok := j.keys[kid]; ok {
			return k, nil
		}
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

func (j *jwks) refresh() error {
	j.fetched = time.Now()
	resp, err := j.client.Get(j.url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}

	var set struct {
		Keys []struct {
			Kid string `json:"kid"`
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&set); err != nil {
		return err
	}

	keys := make(map[string]*rsa.PublicKey)
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := base64.RawURLEncoding.DecodeString(k.N)
		e, err2 := base64.RawURLEncoding.DecodeString(k.E)
		if err1 != nil || err2 != nil {
			continue
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	j.keys = keys
	return nil
}
