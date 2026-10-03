package client

import "fmt"

// Client 仅演示构造边界和配置快照，不实现网络或重试功能。
// 零值不受支持，应通过 New 创建。
type Client struct {
	options Options
}

// New 先组装并校验配置，再保存独立快照。
// 实际库需要的资源在校验成功后初始化，并沿用原错误和清理契约。
func New(opts ...Option) (*Client, error) {
	o := NewOptions(opts...)
	if err := o.Validate(); err != nil {
		return nil, fmt.Errorf("client: invalid options: %w", err)
	}
	return &Client{options: o.clone()}, nil
}

// Options 返回配置副本，修改它不会改变组件内部配置。
// 此方法展示所有权边界，不要求其它库自动新增同名 API。
func (c *Client) Options() *Options {
	o := c.options.clone()
	return &o
}
