package mcpserver

import (
	"context"
	"strings"
	"testing"
)

func TestCropDownloadDialRejectsPrivateAddress(t *testing.T) {
	_, err := cropDialContext(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil || !strings.Contains(err.Error(), "不允许连接内网地址") {
		t.Fatalf("private connection should be rejected: %v", err)
	}
}
