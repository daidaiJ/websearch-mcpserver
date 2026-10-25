package mcpserver

import (
	"reflect"

	"websearch/pkg/i18n"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MCP 工具定义的本地化：客户端看到的工具 = name + description + inputSchema，
// 两者都是用户可见文案。工具描述在 server.go 经 i18n.T 取 mcp.tool.* 条目；
// 参数描述固化在结构体标签里（标签写的是 mcp.param.* 的 key），只能在注册时
// 按当前语言生成 Schema。
//
// 语言取进程默认（config.language 解析结果），在工具注册时固化，切换需重启进程。

// addTool 注册工具并注入本地化后的输入 Schema（见 localizeInputSchema）。
// 除 Schema 来源外与 mcp.AddTool 等价。
func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	if tool.InputSchema == nil {
		tool.InputSchema = localizeInputSchema[In]()
	}
	mcp.AddTool(server, tool, handler)
}

// localizeInputSchema 推断 In 类型的输入 Schema 并逐层渲染 description。
// 返回 nil 时 SDK 回退到自行推断的 Schema（描述会原样露出 key），
// 所以这里只在推断失败（类型不受支持）时才返回 nil。
func localizeInputSchema[T any]() any {
	rt := reflect.TypeFor[T]()
	for rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	schema, err := jsonschema.ForType(rt, &jsonschema.ForOptions{})
	if err != nil {
		return nil
	}
	localizeSchemaNode(schema)
	return schema
}

// localizeSchemaNode 递归渲染 Schema 树中的 description。
// 未登记的 key 由 i18n 返回 key 本身，漏登记由 pkg/i18n 的目录审计拦截。
func localizeSchemaNode(s *jsonschema.Schema) {
	if s == nil {
		return
	}
	if s.Description != "" {
		s.Description = i18n.T(s.Description)
	}
	for _, child := range s.PrefixItems {
		localizeSchemaNode(child)
	}
	for _, child := range s.ItemsArray {
		localizeSchemaNode(child)
	}
	for _, group := range []map[string]*jsonschema.Schema{s.Defs, s.Definitions, s.Properties, s.PatternProperties, s.DependentSchemas} {
		for _, child := range group {
			localizeSchemaNode(child)
		}
	}
	for _, child := range []*jsonschema.Schema{
		s.Items, s.AdditionalItems, s.Contains, s.UnevaluatedItems,
		s.AdditionalProperties, s.PropertyNames, s.UnevaluatedProperties,
		s.Not, s.If, s.Then, s.Else,
	} {
		localizeSchemaNode(child)
	}
	for _, group := range [][]*jsonschema.Schema{s.AllOf, s.AnyOf, s.OneOf} {
		for _, child := range group {
			localizeSchemaNode(child)
		}
	}
}
