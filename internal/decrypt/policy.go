package decrypt

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"filippo.io/age"
	"filippo.io/age/agessh"
	sopsage "github.com/getsops/sops/v3/age"
	sopsconfig "github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/pgp"
	yamlstore "github.com/getsops/sops/v3/stores/yaml"
	"go.yaml.in/yaml/v3"
)

const (
	maxKeyGroups           = 4
	maxDocumentRecipients  = 16
	maxCandidateRecipients = 256
)

// ValidateCandidate checks the entire candidate before any credential discovery
// or key recovery. Remote backends are deliberately unsupported: SOPS' stable
// decrypt API cannot enforce deadlines on their calls. Limits count all keys,
// including fallback recipients that might never be tried.
func (SOPS) ValidateCandidate(ctx context.Context, documents [][]byte) error {
	total := 0
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := validateDocument(document)
		if err != nil {
			return errors.New("SOPS metadata violates local backend or work policy")
		}
		total += n
		if total > maxCandidateRecipients {
			return errors.New("SOPS candidate exceeds recipient work budget")
		}
	}
	return ctx.Err()
}

func validateDocument(encrypted []byte) (int, error) {
	// Use the same YAML implementation as the pinned SOPS store. Reject aliases,
	// merges, duplicate keys and multiple documents before SOPS can expand them.
	dec := yaml.NewDecoder(bytes.NewReader(encrypted))
	var doc, extra yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return 0, err
	}
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return 0, errors.New("expected one encrypted YAML document")
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return 0, errors.New("expected encrypted YAML mapping")
	}
	if err := validateNode(&doc, 0); err != nil {
		return 0, err
	}
	store := yamlstore.NewStore(&sopsconfig.YAMLStoreConfig{})
	// Loading parses metadata but does not recover keys. Validate the exact
	// backend types SOPS will dispatch, including case-insensitive metadata keys.
	tree, err := store.LoadEncryptedFile(encrypted)
	if err != nil {
		return 0, err
	}
	groups := tree.Metadata.KeyGroups
	if len(groups) == 0 || len(groups) > maxKeyGroups {
		return 0, errors.New("invalid SOPS key group count")
	}
	total := 0
	for _, group := range groups {
		if len(group) == 0 {
			return 0, errors.New("empty SOPS key group")
		}
		total += len(group)
		if total > maxDocumentRecipients {
			return 0, errors.New("too many SOPS recipients")
		}
		for _, key := range group {
			switch key := key.(type) {
			case *sopsage.MasterKey:
				if !localAgeRecipient(key.Recipient) {
					return 0, errors.New("unsupported Age recipient")
				}
			case *pgp.MasterKey:
				// Keep existing local keyring/GnuPG support. Credential helpers
				// and agents are configured by the operator, never by Git.
			default:
				return 0, errors.New("remote SOPS backend is disabled")
			}
		}
	}
	return total, nil
}

func localAgeRecipient(recipient string) bool {
	if _, err := age.ParseX25519Recipient(recipient); err == nil {
		return true
	}
	if _, err := age.ParseHybridRecipient(recipient); err == nil {
		return true
	}
	_, err := agessh.ParseRecipient(recipient)
	return err == nil
}

func validateNode(node *yaml.Node, depth int) error {
	if depth > 32 || node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errors.New("unsupported encrypted YAML structure")
	}
	if node.Kind == yaml.SequenceNode && len(node.Content) > maxDocumentRecipients {
		return errors.New("encrypted YAML sequence exceeds work limit")
	}
	if node.Kind == yaml.MappingNode {
		seen := make(map[string]bool, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			name := key.Value
			// Root secret names are case-sensitive, but SOPS decodes nested
			// metadata using case-insensitive mapstructure matching. Reject
			// colliding spellings so repeated parses cannot select different
			// recipients based on Go map iteration order.
			if depth >= 2 {
				for _, r := range name {
					if r > 127 {
						return errors.New("non-ASCII metadata key")
					}
				}
				name = strings.ToLower(name)
				switch name {
				case "kms", "gcp_kms", "hckms", "azure_kv", "hc_vault":
					if value.Tag != "!!null" &&
						(value.Kind != yaml.SequenceNode || len(value.Content) != 0) {
						return errors.New("remote SOPS metadata is disabled")
					}
				}
			}
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" ||
				key.Value == "<<" || seen[name] {
				return errors.New("ambiguous encrypted YAML key")
			}
			seen[name] = true
			if strings.EqualFold(key.Value, "key_groups") && len(value.Content) > maxKeyGroups {
				return errors.New("too many encoded SOPS key groups")
			}
		}
	}
	for _, child := range node.Content {
		if err := validateNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
