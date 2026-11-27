package dashboard

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

func settingsYAMLDocument(b []byte) (*yaml.Node, error) {
	doc := &yaml.Node{}
	decoder := yaml.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(doc); errors.Is(err, io.EOF) {
		// Empty and comment-only overlay files are valid and can receive their first setting.
		doc.Kind = yaml.DocumentNode
		doc.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map", HeadComment: strings.TrimSpace(string(b))}}
	} else if err != nil || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errSettingsParse
	}
	if err := decoder.Decode(&yaml.Node{}); !errors.Is(err, io.EOF) {
		return nil, errSettingsParse
	}
	return doc, nil
}

func yamlMappingIndex(node *yaml.Node, key string) int {
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			if strings.EqualFold(node.Content[i].Value, key) {
				return i
			}
		}
	}
	return -1
}

func yamlPath(doc *yaml.Node, path string) *yaml.Node {
	node := doc.Content[0]
	for _, key := range strings.Split(path, ".") {
		i := yamlMappingIndex(node, key)
		if i < 0 {
			return nil
		}
		node = node.Content[i+1]
	}
	return node
}

func anchoredSetting(node *yaml.Node) bool {
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return true
	}
	return slices.ContainsFunc(node.Content, anchoredSetting)
}

func keepYAMLSettingsComments(before, after *yaml.Node) {
	after.HeadComment, after.LineComment, after.FootComment = before.HeadComment, before.LineComment, before.FootComment
	if before.Tag == after.Tag {
		after.Style = before.Style
	}
	if before.Kind != yaml.MappingNode || after.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(after.Content); i += 2 {
		j := yamlMappingIndex(before, after.Content[i].Value)
		if j >= 0 {
			after.Content[i] = before.Content[j]
			keepYAMLSettingsComments(before.Content[j+1], after.Content[i+1])
		}
	}
}

// A nil value removes a leaf, allowing the loader to restore its inherited or
// default value. Anchored nodes are left alone because aliases can reference them
// from settings outside the allowlist.
func editYAMLSetting(doc *yaml.Node, path string, value any) error {
	node := doc.Content[0]
	keys := strings.Split(path, ".")
	for _, key := range keys[:len(keys)-1] {
		if node.Kind != yaml.MappingNode || node.Anchor != "" {
			return errSettingsParse
		}
		i := yamlMappingIndex(node, key)
		if i < 0 {
			if value == nil {
				return nil
			}
			node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
			i = len(node.Content) - 2
		}
		node = node.Content[i+1]
	}
	if node.Kind != yaml.MappingNode || node.Anchor != "" {
		return errSettingsParse
	}
	key := keys[len(keys)-1]
	i := yamlMappingIndex(node, key)
	if i >= 0 && anchoredSetting(node.Content[i+1]) {
		return errSettingsParse
	}
	if value == nil {
		if i >= 0 {
			node.Content = slices.Delete(node.Content, i, i+2)
		}
		return nil
	}
	var replacement yaml.Node
	if prior, restoring := value.(*yaml.Node); restoring {
		replacement = *prior
	} else if err := replacement.Encode(value); err != nil {
		return errSettingsParse
	}
	if i < 0 {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &replacement)
	} else {
		keepYAMLSettingsComments(node.Content[i+1], &replacement)
		node.Content[i+1] = &replacement
	}
	return nil
}
