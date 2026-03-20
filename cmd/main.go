package main

import (
	mcpserver "websearch/mcp"
	"websearch/pkg/config"
)

func main() {
	conf, err := config.Load()
	if err != nil {
		panic(err)
	}

	mcpserver.Init(conf)
	mcpserver.RunServer(conf.Port)

}
