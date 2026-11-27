package config

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"

	"github.com/spf13/viper"
)

const credentialAuthToken = "WEBSEARCH_TOKEN"

type credentialFields struct {
	name, path string
	value      *string
	pool       *[]string
}

func (f credentialFields) poolPath() string {
	list := "sk_list"
	if f.name == "GITHUB_TOKEN" {
		list = "token_list"
	}
	return strings.TrimSuffix(f.path, "api_key") + list
}

func credentialCatalog(c *Config) []credentialFields {
	return []credentialFields{
		{"BAIDU_SK", "baidu.api_key", &c.Baidu.APIKey, &c.Baidu.SKList},
		{"TAVILY_SK", "tavily.api_key", &c.Tavily.APIKey, &c.Tavily.SKList},
		{"EXA_API_KEY", "exa.api_key", &c.Exa.APIKey, &c.Exa.SKList},
		{"ANYSEARCH_API_KEY", "anysearch.api_key", &c.Anysearch.APIKey, &c.Anysearch.SKList},
		{"DOUBAO_SEARCH_API_KEY", "doubao.api_key", &c.Doubao.APIKey, &c.Doubao.SKList},
		{"TENCENTCLOUD_WSA_APIKEY", "yuanbao.api_key", &c.Yuanbao.APIKey, &c.Yuanbao.SKList},
		{"TINYFISH_API_KEY", "tinyfish.api_key", &c.TinyFish.APIKey, &c.TinyFish.SKList},
		{"KEENABLE_API_KEY", "keenable.api_key", &c.Keenable.APIKey, &c.Keenable.SKList},
		{"GITHUB_TOKEN", "github.api_key", &c.GitHub.APIKey, &c.GitHub.TokenList},
		{"JINA_API_KEY", "jina.api_key", &c.Jina.APIKey, nil},
		{"MINERU_TOKEN", "pdf_parser.mineru_token", &c.PDFParser.MinerUToken, nil},
		{"LLM_API_KEY", "llm.api_key", &c.LLM.APIKey, nil},
		{"SEMANTIC_SCHOLAR_API_KEY", "academic.semantic_scholar_api_key", &c.Academic.SemanticScholarAPIKey, nil},
		{"OPENALEX_API_KEY", "academic.openalex_api_key", &c.Academic.OpenAlexAPIKey, nil},
		{"UNPAYWALL_EMAIL", "academic.unpaywall_email", &c.Academic.UnpaywallEmail, nil},
		{credentialAuthToken, "auth_token", &c.AuthToken, nil},
	}
}

type credentialSnapshot struct {
	base      map[string][]credentialSlot
	running   map[string][]credentialSlot
	listed    map[string]bool
	sensitive []string
}

// applyCredentialEnv also captures the values and provenance at startup, before
// the private overlay takes effect. Runtime status never re-reads the environment.
func applyCredentialEnv(c *Config, v *viper.Viper) {
	sources := map[string]string{}
	for _, field := range credentialCatalog(c) {
		env := os.Getenv(field.name)
		if env == "" && field.name == "DOUBAO_SEARCH_API_KEY" {
			env = os.Getenv("ASK_ECHO_SEARCH_INFINITY_API_KEY")
		}
		if env != "" {
			*field.value = env
			sources[field.path] = "env"
		} else if v != nil && *field.value != "" && os.Getenv("APP_"+strings.ToUpper(field.path)) == *field.value {
			// Preserve provenance for values read by Viper's existing AutomaticEnv.
			sources[field.path] = "env"
		}
		if v != nil && field.pool != nil && len(*field.pool) != 0 {
			path := field.poolPath()
			if env := os.Getenv("APP_" + strings.ToUpper(path)); env != "" {
				sources[path] = "env"
			}
		}
	}
	c.credentials = captureCredentials(c, sources)
}

func captureCredentials(c *Config, sources map[string]string) *credentialSnapshot {
	snapshot := &credentialSnapshot{base: map[string][]credentialSlot{}, listed: map[string]bool{}}
	for _, field := range credentialCatalog(c) {
		values := []string{*field.value}
		source := sources[field.path]
		snapshot.sensitive = append(snapshot.sensitive, *field.value)
		if field.pool != nil && len(*field.pool) != 0 {
			values = *field.pool
			snapshot.listed[field.name] = true
			source = sources[field.poolPath()]
		}
		if source == "" {
			source = "config"
		}
		seen := map[string]bool{}
		for _, value := range values {
			if field.pool != nil {
				value = strings.TrimSpace(value)
			}
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			snapshot.sensitive = append(snapshot.sensitive, value)
			snapshot.base[field.name] = append(snapshot.base[field.name], credentialSlot{
				ID: credentialReference(field.name, source, value), Value: value, source: source,
			})
		}
	}
	snapshot.running = snapshot.base
	return snapshot
}

// Only the private file uses these matching references. HTTP slot IDs are
// independently generated random values and never encode a credential.
func credentialReference(name, source, value string) string {
	digest := sha256.Sum256([]byte(name + "\x00" + source + "\x00" + value))
	return hex.EncodeToString(digest[:])
}

func (c Config) EffectiveCredentialFlags() map[string]bool {
	flags := make(map[string]bool, 16)
	snapshot := c.credentials
	if snapshot == nil {
		snapshot = captureCredentials(&c, nil)
	}
	for _, field := range credentialCatalog(&c) {
		flags[field.name] = len(snapshot.running[field.name]) > 0
	}
	return flags
}
