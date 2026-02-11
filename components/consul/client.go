package consul

import (
	"encoding/json"
	"github.com/hashicorp/consul/api"
)

type Client struct {
	EndPoint string
	c        *api.Client
}

func NewClient(endPoint string) (*Client, error) {
	config := api.DefaultConfig()
	config.Address = endPoint
	client, err := api.NewClient(config)
	if err != nil {
		return nil, err
	}

	return &Client{EndPoint: endPoint, c: client}, nil
}

func (c *Client) GetConfigs(path string) (map[string]any, error) {
	get, _, err := c.c.KV().Get(path, nil)
	if err != nil {
		return nil, err
	}
	res := map[string]any{}
	if get == nil {
		return res, nil
	}

	err = json.Unmarshal(get.Value, &res)

	return res, err
}
