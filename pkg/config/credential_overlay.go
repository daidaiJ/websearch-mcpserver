package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
)

type credentialSlot struct {
	ID    string `json:"id"`
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	First bool   `json:"first,omitempty"`
	// Replaces anchors this overlay slot to a removed config or environment slot.
	Replaces string `json:"replaces,omitempty"`
	source   string
}

type credentialPatch struct {
	HideBase bool              `json:"hide_base,omitempty"`
	Removed  []string          `json:"removed,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Slots    []credentialSlot  `json:"slots,omitempty"`
}

type credentialDocument map[string]json.RawMessage

var ErrCredentialFile = errors.New("invalid credentials file")

func decodeCredentialDocument(data []byte) (credentialDocument, error) {
	doc := credentialDocument{}
	if data != nil && (json.Unmarshal(data, &doc) != nil || doc == nil) {
		return nil, ErrCredentialFile
	}
	return doc, nil
}

func (doc credentialDocument) patch(name string, snapshot *credentialSnapshot, multi bool) (credentialPatch, error) {
	var patch credentialPatch
	raw, exists := doc[name]
	if !exists {
		return patch, nil
	}
	var legacy string
	if json.Unmarshal(raw, &legacy) == nil {
		if multi {
			legacy = strings.TrimSpace(legacy)
		}
		if legacy == "" {
			return patch, nil
		}
		// Older versions replaced the main/env credential. GitHub alone
		// prepended its saved token when token_list was explicitly configured.
		merge := name == "GITHUB_TOKEN" && snapshot.listed[name]
		patch.HideBase = !merge
		if merge && slices.ContainsFunc(snapshot.base[name], func(slot credentialSlot) bool { return slot.Value == legacy }) {
			return patch, nil
		}
		patch.Slots = []credentialSlot{{ID: credentialReference(name, "overlay", legacy), Value: legacy, First: merge}}
		return patch, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&patch) != nil || (!multi && len(patch.Slots) > 1) {
		return patch, ErrCredentialFile
	}
	seen := map[string]bool{}
	replaced := map[string]bool{}
	for i := range patch.Slots {
		slot := &patch.Slots[i]
		if slot.ID == "" || seen[slot.ID] || strings.TrimSpace(slot.Value) == "" || strings.ContainsAny(slot.Value, "\r\n") {
			return patch, ErrCredentialFile
		}
		if multi {
			slot.Value = strings.TrimSpace(slot.Value)
		}
		if slot.Replaces != "" {
			if !multi || slot.First || replaced[slot.Replaces] || !slices.Contains(patch.Removed, slot.Replaces) {
				return patch, ErrCredentialFile
			}
			replaced[slot.Replaces] = true
		}
		seen[slot.ID] = true
	}
	return patch, nil
}

func (p credentialPatch) effective(base []credentialSlot) []credentialSlot {
	var out []credentialSlot
	seen := map[string]bool{}
	appendSlot := func(slot credentialSlot) {
		if !seen[slot.Value] {
			out = append(out, slot)
			seen[slot.Value] = true
		}
	}
	replacements := map[string]credentialSlot{}
	for _, slot := range p.Slots {
		if slot.First {
			slot.source = "overlay"
			appendSlot(slot)
		} else if slot.Replaces != "" {
			slot.source = "overlay"
			replacements[slot.Replaces] = slot
		}
	}
	if !p.HideBase {
		for _, slot := range base {
			if replacement, ok := replacements[slot.ID]; ok {
				appendSlot(replacement)
				delete(replacements, slot.ID)
			} else if !slices.Contains(p.Removed, slot.ID) {
				slot.Label = p.Labels[slot.ID]
				appendSlot(slot)
			}
		}
	}
	for _, slot := range p.Slots {
		_, unmatched := replacements[slot.Replaces]
		if !slot.First && (slot.Replaces == "" || unmatched) {
			slot.source = "overlay"
			appendSlot(slot)
		}
	}
	return out
}

func applyDashboardSecretsAt(c *Config, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	doc, err := decodeCredentialDocument(data)
	if err != nil {
		return
	}
	fields := credentialCatalog(c)
	patches := map[string]credentialPatch{}
	for _, field := range fields {
		if _, present := doc[field.name]; !present || field.name == credentialAuthToken {
			continue
		}
		patch, err := doc.patch(field.name, c.credentials, field.pool != nil)
		if err != nil {
			return
		}
		patches[field.name] = patch
	}
	c.credentials.running = make(map[string][]credentialSlot, len(fields))
	for _, field := range fields {
		base := c.credentials.base[field.name]
		patch, present := patches[field.name]
		if !present {
			c.credentials.running[field.name] = base
			continue
		}
		for _, slot := range patch.Slots {
			c.credentials.sensitive = append(c.credentials.sensitive, slot.Value)
		}
		if !patch.HideBase && len(patch.Slots) == 0 && len(patch.Removed) == 0 && len(patch.Labels) == 0 {
			c.credentials.running[field.name] = base
			continue
		}
		slots := patch.effective(base)
		c.credentials.running[field.name] = slots
		*field.value = ""
		if field.pool != nil {
			*field.pool = nil
		}
		if len(slots) > 0 {
			*field.value = slots[0].Value
		}
		if field.pool != nil && len(slots) > 1 {
			for _, slot := range slots {
				*field.pool = append(*field.pool, slot.Value)
			}
		}
	}
}
