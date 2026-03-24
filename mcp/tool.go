package mcpserver

import (
	"context"
	"fmt"
	"websearch/pkg/config"
	"websearch/pkg/search"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type SearchParams struct {
	Query string `json:"query"`
}

var searchapi search.SearchInf

func Init(conf config.Config) {
	searchapi = search.NewBaiduSeach(conf.BaiduSK, conf.BlackListHost)
}

func WebSearch(ctx context.Context, req *mcp.CallToolRequest, params *SearchParams) (*mcp.CallToolResult, any, error) {
	if searchapi != nil {
		ret, err := searchapi.Search(params.Query, false)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: ret}}}, nil, nil
	}
	return nil, nil, fmt.Errorf("api 初始化未完成")
}
