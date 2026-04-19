package vault

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type Secrets map[string]string

func (s Secrets) Require(key string) (string, error) {
	v, ok := s[key]
	if !ok || v == "" {
		return "", fmt.Errorf("vault: missing required secret %q", key)
	}
	return v, nil
}

// Load reads KV v2 secrets from Vault.
// In K8s it reads /vault/secrets/<service> written by the Agent sidecar.
// In local dev it calls the Vault HTTP API using VAULT_ADDR + VAULT_TOKEN.
func Load(service string) (Secrets, error) {
	addr := envOr("VAULT_ADDR", "http://localhost:8200")
	token := os.Getenv("VAULT_TOKEN")

	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/v1/secret/data/newsroom/%s", addr, service), nil)
	if err != nil {
		return nil, fmt.Errorf("vault: build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault: connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault: HTTP %d reading newsroom/%s", resp.StatusCode, service)
	}

	var body struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("vault: decode: %w", err)
	}
	return Secrets(body.Data.Data), nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
