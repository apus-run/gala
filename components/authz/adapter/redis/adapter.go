// Copyright 2017 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package redisadapter stores Casbin policies in a Redis list and publishes
// change hints over pub/sub | 将 Casbin 策略存入 Redis 列表，并通过 pub/sub 发布变更提示。
//
// # Notes | 说明
//
// This is casbin's redis-adapter v3.6.0, reworked here for Casbin v3 and
// go-redis v9. All rules live as JSON strings in one list (default key
// casbin_rules). Configure a connection, or inject a Client which stays
// caller-owned and is never closed by the adapter. [Watcher] delivers
// invalidation hints, not policies; reconnects can lose events, so pair it
// with periodic reloads.
// 本包是 casbin redis-adapter v3.6.0 的改造版，适配 Casbin v3 与 go-redis v9。
// 所有规则以 JSON 字符串存于一个列表（默认 key casbin_rules）。可配置连接，
// 也可注入 Client——Client 归调用方所有，适配器不会关闭它。[Watcher] 传递的是
// 失效提示而非策略；重连可能丢事件，需配合周期性重载。
//
// # Features | 功能
//
// - Adapter for casbin.persist backed by a Redis list | 基于 Redis 列表的 casbin.persist Adapter
// - Config with an injected go-redis client or an own connection | Config 支持注入 go-redis 客户端或自建连接
// - Watcher broadcasting policy invalidation on a channel | Watcher 在频道上广播策略失效
//
// # Example | 示例
//
//	a, err := NewAdapter(&Config{Client: rdb})
//	w, err := NewWatcher(ctx, rdb, "casbin_policy")
package redisadapter

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/casbin/casbin/v3/model"
	"github.com/casbin/casbin/v3/persist"
	"github.com/redis/go-redis/v9"
)

// CasbinRule is one policy line stored as JSON in the list
// | CasbinRule 是列表中以 JSON 存储的一条策略行.
type CasbinRule struct {
	// PType is the policy type, e.g. p or g | 策略类型，如 p 或 g.
	PType string
	// V0 through V5 are the rule values | 规则值 v0–v5.
	V0 string
	V1 string
	V2 string
	V3 string
	V4 string
	V5 string
}

// Config represents the configuration for the Redis adapter
// | Config 是 Redis 适配器的配置.
type Config struct {
	// Network is the network type, e.g. "tcp", "unix" | 网络类型，如 "tcp"、"unix".
	Network string
	// Address is the Redis server address, e.g. "127.0.0.1:6379" | Redis 服务器地址，如 "127.0.0.1:6379".
	Address string
	// Key is the Redis key storing Casbin rules (default "casbin_rules") | 存放 Casbin 规则的 Redis key（默认 "casbin_rules"）.
	Key string
	// Username for Redis authentication (optional) | Redis 认证用户名（可选）.
	Username string
	// Password for Redis authentication (optional) | Redis 认证密码（可选）.
	Password string
	// TLSConfig for secure connections (optional) | 安全连接的 TLS 配置（可选）.
	TLSConfig *tls.Config
	// Client is an existing go-redis client (optional).
	// If provided, Network, Address, Username, Password, and TLSConfig are
	// ignored. The adapter does not close an injected client.
	// Client 是既有的 go-redis 客户端（可选）。提供后 Network、Address、
	// Username、Password、TLSConfig 均被忽略。适配器不会关闭注入的客户端。
	Client redis.Cmdable
}

// Adapter represents the Redis adapter for policy storage
// | Adapter 是基于 Redis 的策略存储适配器.
type Adapter struct {
	network    string
	address    string
	key        string
	username   string
	password   string
	tlsConfig  *tls.Config
	client     redis.Cmdable
	closeFn    func() error
	isFiltered atomic.Bool
}

// finalizer closes only the client this adapter created
// | finalizer 只关闭本适配器自建的客户端.
func finalizer(a *Adapter) {
	if a.closeFn != nil {
		_ = a.closeFn()
	}
}

// NewAdapter creates a Redis adapter from the provided configuration
// | NewAdapter 按给定配置创建 Redis 适配器.
func NewAdapter(config *Config) (*Adapter, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	a := &Adapter{}

	// fall back to the default key when none is provided
	// 未提供 key 时使用默认 key
	if config.Key == "" {
		a.key = "casbin_rules"
	} else {
		a.key = config.Key
	}

	if config.Client != nil && nilClient(config.Client) {
		return nil, errors.New("typed nil Redis client")
	}
	if config.Client != nil {
		a.client = config.Client
	} else {
		if config.Network == "" {
			return nil, errors.New("network is required when not using a client")
		}
		if config.Address == "" {
			return nil, errors.New("address is required when not using a client")
		}

		a.network = config.Network
		a.address = config.Address
		a.username = config.Username
		a.password = config.Password
		a.tlsConfig = config.TLSConfig

		if err := a.open(); err != nil {
			return nil, err
		}
	}

	if a.closeFn != nil {
		runtime.SetFinalizer(a, finalizer)
	}
	return a, nil
}

// NewAdapterBasic is the basic constructor for Adapter.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterBasic 是 Adapter 的基础构造函数。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterBasic(network string, address string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address})
}

// NewAdapterWithUser creates an adapter with user credentials.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterWithUser 用用户名密码创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterWithUser(network string, address string, username string, password string) (*Adapter, error) {
	return NewAdapter(&Config{
		Network:  network,
		Address:  address,
		Username: username,
		Password: password,
	})
}

// NewAdapterWithPassword creates an adapter with password authentication.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterWithPassword 用密码认证创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterWithPassword(network string, address string, password string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address, Password: password})
}

// NewAdapterWithKey creates an adapter with a custom key.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterWithKey 用自定义 key 创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterWithKey(network string, address string, key string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address, Key: key})
}

// NewAdapterWithClient creates an adapter over an existing go-redis client.
// Deprecated: Use NewAdapter with Config struct instead.
// The adapter does not close the client.
// NewAdapterWithClient 用既有的 go-redis 客户端创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。适配器不会关闭该客户端。
func NewAdapterWithClient(client redis.Cmdable) (*Adapter, error) {
	return NewAdapter(&Config{Client: client})
}

// NewAdapterWithClientAndOptions creates an adapter with a client and options.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterWithClientAndOptions 用客户端加选项创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterWithClientAndOptions(client redis.Cmdable, options ...Option) (*Adapter, error) {
	a, err := NewAdapter(&Config{Client: client})
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		option.apply(a)
	}
	return a, nil
}

// Option customizes an Adapter before construction | Option 在构造前自定义 Adapter.
type Option interface {
	apply(*Adapter)
}

// OptionFunc adapts a plain function to Option | OptionFunc 把普通函数适配成 Option.
type OptionFunc func(*Adapter)

func (f OptionFunc) apply(o *Adapter) {
	f(o)
}

// NewAdapterWithOption creates an adapter with the options pattern.
// Deprecated: Use NewAdapter with Config struct instead.
// NewAdapterWithOption 用选项模式创建适配器。
// 已废弃：请改用带 Config 结构体的 NewAdapter。
func NewAdapterWithOption(options ...Option) (*Adapter, error) {
	a := &Adapter{}
	for _, option := range options {
		option.apply(a)
	}
	return NewAdapter(&Config{
		Network:   a.network,
		Address:   a.address,
		Key:       a.key,
		Username:  a.username,
		Password:  a.password,
		TLSConfig: a.tlsConfig,
	})
}

// WithAddress sets the Redis server address | WithAddress 设置 Redis 服务器地址.
func WithAddress(address string) Option {
	return OptionFunc(func(a *Adapter) { a.address = address })
}

// WithUsername sets the auth username | WithUsername 设置认证用户名.
func WithUsername(username string) Option {
	return OptionFunc(func(a *Adapter) { a.username = username })
}

// WithPassword sets the auth password | WithPassword 设置认证密码.
func WithPassword(password string) Option {
	return OptionFunc(func(a *Adapter) { a.password = password })
}

// WithNetwork sets the network type | WithNetwork 设置网络类型.
func WithNetwork(network string) Option {
	return OptionFunc(func(a *Adapter) { a.network = network })
}

// WithKey sets the rules key | WithKey 设置规则存放的 key.
func WithKey(key string) Option {
	return OptionFunc(func(a *Adapter) { a.key = key })
}

// WithTls sets the TLS configuration | WithTls 设置 TLS 配置.
func WithTls(tlsConfig *tls.Config) Option {
	return OptionFunc(func(a *Adapter) { a.tlsConfig = tlsConfig })
}

func (a *Adapter) open() error {
	client := redis.NewClient(&redis.Options{
		Network:   a.network,
		Addr:      a.address,
		Username:  a.username,
		Password:  a.password,
		TLSConfig: a.tlsConfig,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		_ = client.Close()
		return err
	}
	a.client = client
	a.closeFn = client.Close
	return nil
}

func (c *CasbinRule) toStringPolicy() []string {
	fields := []string{c.V0, c.V1, c.V2, c.V3, c.V4, c.V5}
	// Only trailing padding can be trimmed; interior empty values are positional.
	// 只能裁掉末尾的填充；中间的空值是有位置含义的。
	end := len(fields)
	for end > 0 && fields[end-1] == "" {
		end--
	}
	return append([]string{c.PType}, fields[:end]...)
}

func decodePolicyLine(text string) (CasbinRule, error) {
	var line CasbinRule
	if err := json.Unmarshal([]byte(text), &line); err != nil {
		return line, fmt.Errorf("redis adapter: decode policy: %w", err)
	}
	if line.PType == "" {
		return line, errors.New("redis adapter: empty policy type")
	}
	return line, nil
}

func loadPolicyLine(line CasbinRule, m model.Model) error {
	return persist.LoadPolicyArray(line.toStringPolicy(), m)
}

// LoadPolicy loads the policy from Redis | LoadPolicy 从 Redis 加载策略.
func (a *Adapter) LoadPolicy(m model.Model) error {
	ctx := context.Background()
	values, err := a.client.LRange(ctx, a.key, 0, -1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	for _, text := range values {
		line, err := decodePolicyLine(text)
		if err != nil {
			return err
		}
		if err := loadPolicyLine(line, m); err != nil {
			return err
		}
	}
	a.isFiltered.Store(false)
	return nil
}

func savePolicyLine(ptype string, rule []string) CasbinRule {
	line := CasbinRule{PType: ptype}
	if len(rule) > 0 {
		line.V0 = rule[0]
	}
	if len(rule) > 1 {
		line.V1 = rule[1]
	}
	if len(rule) > 2 {
		line.V2 = rule[2]
	}
	if len(rule) > 3 {
		line.V3 = rule[3]
	}
	if len(rule) > 4 {
		line.V4 = rule[4]
	}
	if len(rule) > 5 {
		line.V5 = rule[5]
	}
	return line
}

func (a *Adapter) push(values [][]byte) error {
	if len(values) == 0 {
		return nil
	}
	args := make([]any, len(values))
	for i, value := range values {
		args[i] = value
	}
	return a.client.RPush(context.Background(), a.key, args...).Err()
}

// SavePolicy saves the whole policy back to Redis | SavePolicy 把整份策略保存回 Redis.
func (a *Adapter) SavePolicy(m model.Model) error {
	var texts [][]byte
	for _, section := range []string{"p", "g"} {
		for ptype, ast := range m[section] {
			for _, rule := range ast.Policy {
				text, err := json.Marshal(savePolicyLine(ptype, rule))
				if err != nil {
					return err
				}
				texts = append(texts, text)
			}
		}
	}
	args := make([]any, len(texts))
	for i, text := range texts {
		args[i] = text
	}
	if len(args) == 0 {
		// Clearing is one atomic command; no staging key is needed.
		// 清空是一条原子命令，不需要中转 key。
		return a.client.Del(context.Background(), a.key).Err()
	}
	staging, err := snapshotKey(a.key)
	if err != nil {
		return err
	}
	return replacePolicyScript.Run(context.Background(), a.client, []string{a.key, staging}, args...).Err()
}

// AddPolicy adds a policy rule to the storage | AddPolicy 向存储添加一条策略规则.
func (a *Adapter) AddPolicy(sec string, ptype string, rule []string) error {
	text, err := json.Marshal(savePolicyLine(ptype, rule))
	if err != nil {
		return err
	}
	return a.client.RPush(context.Background(), a.key, text).Err()
}

// RemovePolicy removes a policy rule from the storage | RemovePolicy 从存储删除一条策略规则.
func (a *Adapter) RemovePolicy(sec string, ptype string, rule []string) error {
	text, err := json.Marshal(savePolicyLine(ptype, rule))
	if err != nil {
		return err
	}
	return a.client.LRem(context.Background(), a.key, 1, text).Err()
}

// AddPolicies adds multiple policy rules to the storage | AddPolicies 向存储添加多条策略规则.
func (a *Adapter) AddPolicies(sec string, ptype string, rules [][]string) error {
	texts := make([][]byte, 0, len(rules))
	for _, rule := range rules {
		text, err := json.Marshal(savePolicyLine(ptype, rule))
		if err != nil {
			return err
		}
		texts = append(texts, text)
	}
	return a.push(texts)
}

// RemovePolicies removes multiple policy rules from the storage
// | RemovePolicies 从存储删除多条策略规则.
func (a *Adapter) RemovePolicies(sec string, ptype string, rules [][]string) error {
	ctx := context.Background()
	for _, rule := range rules {
		text, err := json.Marshal(savePolicyLine(ptype, rule))
		if err != nil {
			return err
		}
		if err := a.client.LRem(ctx, a.key, 1, text).Err(); err != nil {
			return err
		}
	}
	return nil
}

// IsFiltered returns true if the loaded policy has been filtered
// | IsFiltered 报告已加载的策略是否经过过滤.
func (a *Adapter) IsFiltered() bool {
	return a.isFiltered.Load()
}

// Filter filters rules by ptype and value match | 按 ptype 与值匹配过滤规则.
type Filter struct {
	// PType lists matching policy types | 匹配的策略类型列表.
	PType []string
	// V0 through V5 list matching values | 匹配的值列表 v0–v5.
	V0 []string
	V1 []string
	V2 []string
	V3 []string
	V4 []string
	V5 []string
}

// matches compares decoded fields, never their JSON wire representation
// | matches 比较的是解码后的字段，绝不是 JSON 线上格式.
func (f *Filter) matches(line CasbinRule) bool {
	fields := []string{line.PType, line.V0, line.V1, line.V2, line.V3, line.V4, line.V5}
	values := [][]string{f.PType, f.V0, f.V1, f.V2, f.V3, f.V4, f.V5}
	for i, accepted := range values {
		if len(accepted) > 0 && !slices.Contains(accepted, fields[i]) {
			return false
		}
	}
	return true
}

func (a *Adapter) loadFilteredPolicy(m model.Model, filter *Filter) error {
	values, err := a.client.LRange(context.Background(), a.key, 0, -1).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	for _, text := range values {
		line, err := decodePolicyLine(text)
		if err != nil {
			return err
		}
		if filter.matches(line) {
			if err := loadPolicyLine(line, m); err != nil {
				return err
			}
		}
	}
	return nil
}

// LoadFilteredPolicy loads only the policy rules matching the filter
// | LoadFilteredPolicy 只加载匹配过滤条件的策略规则.
func (a *Adapter) LoadFilteredPolicy(m model.Model, filter any) error {
	if filter == nil {
		return a.LoadPolicy(m)
	}
	var err error
	switch f := filter.(type) {
	case *Filter:
		if f == nil {
			return a.LoadPolicy(m)
		}
		err = a.loadFilteredPolicy(m, f)
	case Filter:
		err = a.loadFilteredPolicy(m, &f)
	default:
		err = fmt.Errorf("invalid filter type")
	}
	if err != nil {
		return err
	}
	a.isFiltered.Store(true)
	return nil
}

// fieldFilter encodes exact field comparisons. Empty values remain wildcards,
// matching Casbin's filter contract; values containing JSON/Lua metacharacters
// need no pattern escaping.
// fieldFilter 编码精确的字段比较。空值仍是通配符，与 Casbin 的过滤契约一致；
// 含 JSON/Lua 元字符的值无需做模式转义。
func fieldFilter(ptype string, fieldIndex int, fieldValues []string) ([]byte, error) {
	if ptype == "" {
		return nil, errors.New("redis adapter: empty policy type")
	}
	if fieldIndex < 0 || fieldIndex > 5 || len(fieldValues) > 6-fieldIndex {
		return nil, errors.New("redis adapter: policy filter field index out of range")
	}
	filter := map[string]string{"PType": ptype}
	for i, value := range fieldValues {
		if value != "" {
			filter[fmt.Sprintf("V%d", fieldIndex+i)] = value
		}
	}
	return json.Marshal(filter)
}

// Both delete and update share literal field matching. Decode/validate every
// stored row before making any change, so malformed data cannot cause partial
// deletion. Permission/runtime failures still follow Redis script semantics.
// 删除与更新共用字面字段匹配。做任何改动前先解码并校验每一行存量数据，
// 坏数据不会造成部分删除。权限/运行时错误仍遵循 Redis 脚本语义。
var filteredPolicyScript = redis.NewScript(`
 local filter = cjson.decode(ARGV[1])
 local rows = redis.call('lrange', KEYS[1], 0, -1)
 local indices, removed = {}, {}
 for i,text in ipairs(rows) do
  local row = cjson.decode(text)
  if type(row) ~= 'table' or type(row.PType) ~= 'string' or row.PType == '' then
   return redis.error_reply('ERR invalid policy type')
  end
  for field=0,5 do
   local value = row['V'..field]
   if value ~= nil and value ~= cjson.null and type(value) ~= 'string' then
    return redis.error_reply('ERR invalid policy field')
   end
  end
  local matches = true
  for field,value in pairs(filter) do
   local actual = row[field]
   if actual == nil or actual == cjson.null then actual = '' end
   if actual ~= value then matches = false; break end
  end
  if matches then table.insert(indices,i-1); table.insert(removed,text) end
 end
 for _,index in ipairs(indices) do redis.call('lset',KEYS[1],index,'__CASBIN_DELETED__') end
 redis.call('lrem',KEYS[1],0,'__CASBIN_DELETED__')
 for i=2,#ARGV do redis.call('rpush',KEYS[1],ARGV[i]) end
 return removed
`)

// RemoveFilteredPolicy removes the policy rules matching the filter from
// storage | RemoveFilteredPolicy 从存储删除匹配过滤条件的策略规则.
func (a *Adapter) RemoveFilteredPolicy(sec, ptype string, fieldIndex int, fieldValues ...string) error {
	filter, err := fieldFilter(ptype, fieldIndex, fieldValues)
	if err != nil {
		return err
	}
	return filteredPolicyScript.Run(context.Background(), a.client, []string{a.key}, filter).Err()
}

var updatePolicyScript = redis.NewScript(`
	local key = KEYS[1]
	local old = ARGV[1]
	local newRule = ARGV[2]
	local r = redis.call('lrange', key, 0, -1)
	for i=1,#r do
		if r[i] == old then
			redis.call('lset', key, i-1, newRule)
			return true
		end
	end
	return false
`)

// UpdatePolicy updates one policy rule in Redis | UpdatePolicy 更新 Redis 中的一条策略规则.
func (a *Adapter) UpdatePolicy(sec string, ptype string, oldRule, newPolicy []string) error {
	textOld, err := json.Marshal(savePolicyLine(ptype, oldRule))
	if err != nil {
		return err
	}
	textNew, err := json.Marshal(savePolicyLine(ptype, newPolicy))
	if err != nil {
		return err
	}
	err = updatePolicyScript.Run(context.Background(), a.client, []string{a.key}, textOld, textNew).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

var updatePoliciesScript = redis.NewScript(`
	local key = KEYS[1]
	local len = #ARGV/2
	local map = {}
	for i = 1, len, 1 do
		map[ARGV[i]] = ARGV[i + len]
	end
	local r = redis.call('lrange', key, 0, -1)
	for i=1,#r do
		if map[r[i]] ~= nil then
			redis.call('lset', key, i-1, map[r[i]])
		end
	end
	return false
`)

// UpdatePolicies updates multiple policy rules in Redis | UpdatePolicies 更新 Redis 中的多条策略规则.
func (a *Adapter) UpdatePolicies(sec string, ptype string, oldRules, newRules [][]string) error {
	if len(oldRules) != len(newRules) {
		return errors.New("oldRules and newRules should have the same length")
	}
	args := make([]any, 0, len(oldRules)+len(newRules))
	for _, oldRule := range oldRules {
		text, err := json.Marshal(savePolicyLine(ptype, oldRule))
		if err != nil {
			return err
		}
		args = append(args, string(text))
	}
	for _, newRule := range newRules {
		text, err := json.Marshal(savePolicyLine(ptype, newRule))
		if err != nil {
			return err
		}
		args = append(args, string(text))
	}
	err := updatePoliciesScript.Run(context.Background(), a.client, []string{a.key}, args...).Err()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// UpdateFilteredPolicies replaces the rules matched by the filter and returns
// the removed rules
// | UpdateFilteredPolicies 替换匹配过滤条件的规则，并返回被删的旧规则.
func (a *Adapter) UpdateFilteredPolicies(sec string, ptype string, newPolicies [][]string, fieldIndex int, fieldValues ...string) ([][]string, error) {
	filter, err := fieldFilter(ptype, fieldIndex, fieldValues)
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, 1+len(newPolicies))
	args = append(args, filter)
	for _, newRule := range newPolicies {
		text, err := json.Marshal(savePolicyLine(ptype, newRule))
		if err != nil {
			return nil, err
		}
		args = append(args, string(text))
	}
	reply, err := filteredPolicyScript.Run(context.Background(), a.client, []string{a.key}, args...).StringSlice()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	ret := make([][]string, 0, len(reply))
	for _, oldRule := range reply {
		line, err := decodePolicyLine(oldRule)
		if err != nil {
			return nil, err
		}
		ret = append(ret, line.toStringPolicy())
	}
	return ret, nil
}

// Close releases only an adapter-owned client. Stop operations before closing.
// Injected clients remain owned by the application.
// Close 只释放适配器自建的客户端。关闭前请先停止操作。
// 注入的客户端始终归应用所有。
func (a *Adapter) Close() error {
	runtime.SetFinalizer(a, nil)
	if a.closeFn == nil {
		return nil
	}
	closeFn := a.closeFn
	a.closeFn = nil
	return closeFn()
}

func nilClient(client any) bool {
	if client == nil {
		return true
	}
	v := reflect.ValueOf(client)
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.Interface:
		return v.IsNil()
	}
	return false
}

// snapshotKey shares the policy key's Redis Cluster hash tag. Braced keys must
// contain a non-empty tag; ambiguous literal braces are rejected before writes.
// snapshotKey 与策略 key 共用 Redis Cluster 的 hash tag。带花括号的 key 必须有
// 非空 tag；含义不明的字面花括号会在写入前被拒绝。
func snapshotKey(key string) (string, error) {
	tag := key
	if start := strings.IndexByte(key, '{'); start >= 0 {
		end := strings.IndexByte(key[start+1:], '}')
		if end <= 0 {
			return "", errors.New("redis adapter: SavePolicy requires a non-empty Redis hash tag for braced keys")
		}
		tag = key[start+1 : start+1+end]
	} else if strings.ContainsRune(key, '}') {
		return "", errors.New("redis adapter: SavePolicy requires a non-empty Redis hash tag for braced keys")
	}
	return fmt.Sprintf("{%s}:gala:authz:snapshot:%s", tag, rand.Text()), nil
}

// Publish only a complete staged List. All fallible construction steps touch
// KEYS[2]; RENAME is the final operation on the live key. Script errors
// therefore cannot leave a prefix of the new policy visible. The temporary
// namespace is private to this adapter. A lost reply can still mean a complete
// commit occurred.
// 只发布完整装配好的 List。所有可能失败的建设步骤都作用于 KEYS[2]；
// RENAME 是对正式 key 的最后一步操作。因此脚本出错不可能让新策略只可见一半。
// 临时命名空间为本适配器私有。回复丢失时也可能提交已经完成。
var replacePolicyScript = redis.NewScript(`
 if redis.call('exists', KEYS[2]) ~= 0 then
  return redis.error_reply('ERR policy staging key already exists')
 end
 local ok, result = pcall(function()
  redis.call('rpush', KEYS[2], ARGV[1])
  redis.call('pexpire', KEYS[2], 300000)
  for i=2,#ARGV do redis.call('rpush', KEYS[2], ARGV[i]) end
  redis.call('persist', KEYS[2])
  return redis.call('rename', KEYS[2], KEYS[1])
 end)
 if not ok then
  redis.pcall('del', KEYS[2])
  return redis.error_reply(tostring(result))
 end
 return result
`)
