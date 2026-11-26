package config

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	ErrCredentialRequest  = errors.New("invalid credential operation")
	ErrCredentialConflict = errors.New("credential state changed")
	ErrCredentialLabel    = errors.New("invalid credential label")
)

type CredentialEditRequest struct {
	Name     string `json:"name"`
	Action   string `json:"action"`
	Value    string `json:"value"`
	Label    string `json:"label"`
	SlotID   string `json:"slot_id"`
	Revision string `json:"revision"`
	Confirm  bool   `json:"confirm"`
}

type CredentialEditResult struct {
	Saved           bool   `json:"saved"`
	Applied         bool   `json:"applied"`
	RestartRequired bool   `json:"restart_required"`
	Backup          bool   `json:"backup"`
	BeforeCount     int    `json:"before_count"`
	AfterCount      int    `json:"after_count"`
	Revision        string `json:"revision"`
}

type CredentialSlotStatus struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Source string `json:"source"`
}

type CredentialStatus struct {
	Count      int                    `json:"count"`
	Configured bool                   `json:"configured"`
	Multi      bool                   `json:"multi"`
	ReadOnly   bool                   `json:"read_only"`
	Sources    []string               `json:"sources"`
	Slots      []CredentialSlotStatus `json:"slots"`
}

type CredentialView struct {
	Credentials    map[string]CredentialStatus `json:"credentials"`
	Pending        map[string]CredentialStatus `json:"pending"`
	PendingRestart bool                        `json:"pending_restart"`
	Revision       string                      `json:"revision"`
}

// CredentialEditor projects the running configuration and current overlay.
// Callers serialize access, including reading and replacing the private file.
type CredentialEditor struct {
	snapshot  *credentialSnapshot
	multi     map[string]bool
	ids       map[credentialSlotIdentity]string
	observed  []byte
	revision  string
	sensitive []string
}

type credentialSlotIdentity struct {
	name, source, id string
}

func NewCredentialEditor(c Config) *CredentialEditor {
	snapshot := c.credentials
	if snapshot == nil {
		snapshot = captureCredentials(&c, nil)
	}
	e := &CredentialEditor{snapshot: snapshot, multi: map[string]bool{}, ids: map[credentialSlotIdentity]string{}}
	for _, field := range credentialCatalog(&c) {
		e.multi[field.name] = field.pool != nil
	}
	e.sensitive = append(slices.Clone(snapshot.sensitive), c.Dashboard.AdminPassword)
	return e
}

// Observe returns a new random file revision only when the original bytes change.
func (e *CredentialEditor) Observe(data []byte) string {
	if e.revision == "" || !bytes.Equal(data, e.observed) || (data == nil) != (e.observed == nil) {
		e.observed = slices.Clone(data)
		e.revision = rand.Text()
	}
	return e.revision
}

func (e *CredentialEditor) slotID(name string, slot credentialSlot) string {
	key := credentialSlotIdentity{name, slot.source, slot.ID}
	if id := e.ids[key]; id != "" {
		return id
	}
	id := rand.Text()
	e.ids[key] = id
	return id
}

func (e *CredentialEditor) view(status map[string][]credentialSlot, sensitive []string) map[string]CredentialStatus {
	out := make(map[string]CredentialStatus, len(e.multi))
	for name, multi := range e.multi {
		slots := status[name]
		row := CredentialStatus{Count: len(slots), Configured: len(slots) > 0, Multi: multi,
			ReadOnly: name == credentialAuthToken, Sources: []string{}, Slots: []CredentialSlotStatus{}}
		for _, slot := range slots {
			if !slices.Contains(row.Sources, slot.source) {
				row.Sources = append(row.Sources, slot.source)
			}
			if !row.ReadOnly {
				label := slot.Label
				if !validCredentialLabel(label, sensitive) {
					label = ""
				}
				row.Slots = append(row.Slots, CredentialSlotStatus{e.slotID(name, slot), label, slot.source})
			}
		}
		out[name] = row
	}
	return out
}

func (e *CredentialEditor) resolve(doc credentialDocument) (map[string][]credentialSlot, []string, error) {
	status := maps.Clone(e.snapshot.base)
	sensitive := slices.Clone(e.sensitive)
	for name, multi := range e.multi {
		if name == credentialAuthToken {
			continue
		}
		patch, err := doc.patch(name, e.snapshot, multi)
		if err != nil {
			return nil, nil, err
		}
		for _, slot := range patch.Slots {
			sensitive = append(sensitive, slot.Value)
		}
		status[name] = patch.effective(status[name])
	}
	return status, sensitive, nil
}

func (e *CredentialEditor) restartRequired(pending map[string][]credentialSlot) bool {
	for name := range e.multi {
		if !slices.EqualFunc(e.snapshot.running[name], pending[name], func(a, b credentialSlot) bool {
			return a.Value == b.Value && a.source == b.source && a.Label == b.Label
		}) {
			return true
		}
	}
	return false
}

func (e *CredentialEditor) View(data []byte) (CredentialView, error) {
	result := CredentialView{Revision: e.Observe(data)}
	var pending map[string][]credentialSlot
	var sensitive []string
	doc, err := decodeCredentialDocument(data)
	if err == nil {
		pending, sensitive, err = e.resolve(doc)
	}
	if err != nil {
		result.Credentials = e.view(e.snapshot.running, e.sensitive)
		return result, err
	}
	result.Credentials = e.view(e.snapshot.running, sensitive)
	result.Pending = e.view(pending, sensitive)
	result.PendingRestart = e.restartRequired(pending)
	return result, nil
}

// Edit builds a complete candidate document. Preview and no-op requests
// return nil bytes, so callers never persist or back up an unchanged file.
func (e *CredentialEditor) Edit(data []byte, req CredentialEditRequest) (CredentialEditResult, []byte, error) {
	result := CredentialEditResult{Revision: e.Observe(data)}
	multi, exists := e.multi[req.Name]
	if !exists || req.Name == credentialAuthToken {
		return result, nil, ErrCredentialRequest
	}
	if req.Revision != "" && req.Revision != result.Revision {
		return result, nil, ErrCredentialConflict
	}
	doc, err := decodeCredentialDocument(data)
	if err != nil {
		return result, nil, err
	}
	pending, sensitive, err := e.resolve(doc)
	if err != nil {
		return result, nil, err
	}
	slots := pending[req.Name]
	result.BeforeCount = len(slots)
	if strings.ContainsAny(req.Value, "\r\n") {
		return result, nil, ErrCredentialRequest
	}
	req.Value = strings.TrimSpace(req.Value)
	req.Label = strings.TrimSpace(req.Label)
	if !validCredentialLabel(req.Label, append(sensitive, req.Value)) {
		return result, nil, ErrCredentialLabel
	}
	legacyGitHub := req.Action == "" && req.Name == "GITHUB_TOKEN"
	if req.Action == "" {
		switch {
		case req.Value == "":
			req.Action = "reset"
		case multi:
			req.Action = "append"
		default:
			req.Action = "set"
		}
	}
	patch, err := doc.patch(req.Name, e.snapshot, multi)
	if err != nil {
		return result, nil, err
	}
	changed, err := e.editPatch(&patch, req, multi, legacyGitHub, slots)
	if err != nil {
		return result, nil, err
	}
	if req.Action == "reset" {
		_, changed = doc[req.Name]
		delete(doc, req.Name)
	} else if changed {
		doc[req.Name], err = json.Marshal(patch)
		if err != nil {
			return result, nil, ErrCredentialFile
		}
	}
	pending[req.Name] = patch.effective(e.snapshot.base[req.Name])
	result.AfterCount = len(pending[req.Name])
	result.RestartRequired = e.restartRequired(pending)
	result.Saved = req.Confirm
	if !changed || !req.Confirm {
		return result, nil, nil
	}
	encoded, err := json.Marshal(doc)
	if err != nil {
		return result, nil, ErrCredentialFile
	}
	result.Applied = true
	return result, encoded, nil
}

func (e *CredentialEditor) editPatch(p *credentialPatch, req CredentialEditRequest, multi, first bool, slots []credentialSlot) (bool, error) {
	switch req.Action {
	case "append", "set":
		if req.Value == "" || req.SlotID != "" || (req.Action == "append") != multi {
			return false, ErrCredentialRequest
		}
		for _, slot := range slots {
			if slot.Value == req.Value && (multi || (slot.source == "overlay" && slot.Label == req.Label)) {
				return false, nil
			}
		}
		if !multi {
			*p = credentialPatch{HideBase: true}
		}
		p.Slots = append(p.Slots, credentialSlot{ID: rand.Text(), Value: req.Value, Label: req.Label, First: first})
		return true, nil
	case "clear", "reset":
		if req.Value != "" || req.Label != "" || req.SlotID != "" {
			return false, ErrCredentialRequest
		}
		clear := req.Action == "clear"
		changed := clear && (!p.HideBase || len(p.Slots) != 0)
		*p = credentialPatch{HideBase: clear}
		return changed, nil
	case "delete", "label":
		if req.Value != "" || req.SlotID == "" || (req.Action == "delete" && req.Label != "") {
			return false, ErrCredentialRequest
		}
		for _, slot := range slots {
			if e.slotID(req.Name, slot) != req.SlotID {
				continue
			}
			if req.Action == "label" && slot.Label == req.Label {
				return false, nil
			}
			if slot.source == "overlay" {
				index := slices.IndexFunc(p.Slots, func(candidate credentialSlot) bool { return candidate.ID == slot.ID })
				if req.Action == "delete" {
					p.Slots = slices.Delete(p.Slots, index, index+1)
				} else {
					p.Slots[index].Label = req.Label
				}
			} else if req.Action == "delete" {
				p.Removed = append(p.Removed, slot.ID)
				delete(p.Labels, slot.ID)
			} else if req.Label == "" {
				delete(p.Labels, slot.ID)
			} else {
				if p.Labels == nil {
					p.Labels = map[string]string{}
				}
				p.Labels[slot.ID] = req.Label
			}
			return true, nil
		}
		return false, ErrCredentialConflict
	default:
		return false, ErrCredentialRequest
	}
}

func validCredentialLabel(label string, sensitive []string) bool {
	if label == "" {
		return true
	}
	if utf8.RuneCountInString(label) > 48 {
		return false
	}
	for _, r := range label {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune(" _-.()（）", r) {
			return false
		}
	}
	for _, value := range sensitive {
		if value == "" {
			continue
		}
		if strings.Contains(label, value) || (len(value) >= 4 &&
			(strings.Contains(label, value[:4]) || strings.Contains(label, value[len(value)-4:]))) {
			return false
		}
	}
	return true
}
