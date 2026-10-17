package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
)

// bwsTokenID is the local secrets store id of the operator's Bitwarden Secrets
// Manager access token.
const bwsTokenID = "BWS_ACCESS_TOKEN"

// bwsSecret is one secret of a Bitwarden Secrets Manager project.
type bwsSecret struct {
	Key          string    `json:"key"`
	Value        string    `json:"value"`
	RevisionDate time.Time `json:"revisionDate"`
}

// bwsSecrets reads the governance project's secrets by key, or nil when the
// local store holds no access token. The token reaches `bws` through its
// environment, and neither a value nor the token is ever printed. A package var
// so tests read a fixture instead of Bitwarden.
var bwsSecrets = func(governance *repocfg.Governance) (map[string]bwsSecret, error) {
	store, err := secrets.Open()
	if err != nil {
		return nil, err
	}
	token, ok := store.Get(bwsTokenID)
	if !ok {
		return nil, nil
	}
	c := exec.Command("bws", "secret", "list", governance.BWSProject, "--output", "json")
	c.Env = append(os.Environ(), "BWS_ACCESS_TOKEN="+token, "BWS_SERVER_URL="+governance.BWSServerURL)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("bws secret list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var list []bwsSecret
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("bws secret list: %w", err)
	}
	byKey := make(map[string]bwsSecret, len(list))
	for _, secret := range list {
		byKey[secret.Key] = secret
	}
	return byKey, nil
}
