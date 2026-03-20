package client

import (
	"resty.dev/v3"
)

var DefaultClient *resty.Client

func init() {
	DefaultClient = resty.New()
}
