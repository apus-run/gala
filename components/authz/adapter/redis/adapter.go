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

// CasbinRule is used to determine which policy line to load.
type CasbinRule struct {
	PType string
	V0    string
	V1    string
	V2    string
	V3    string
	V4    string
	V5    string
}

// Config represents the configuration for the Redis adapter.
type Config struct {
	// Network is the network type, e.g., "tcp", "unix"
	Network string
	// Address is the Redis server address, e.g., "127.0.0.1:6379"
	Address string
	// Key is the Redis key to store Casbin rules (default: "casbin_rules")
	Key string
	// Username for Redis authentication (optional)
	Username string
	// Password for Redis authentication (optional)
	Password string
	// TLSConfig for secure connections (optional)
	TLSConfig *tls.Config
	// Client is an existing go-redis client (optional).
	// If provided, Network, Address, Username, Password, and TLSConfig are ignored.
	// The adapter does not close an injected client.
	Client redis.Cmdable
}

// Adapter represents the Redis adapter for policy storage.
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

// finalizer closes only the client this adapter created.
func finalizer(a *Adapter) {
	if a.closeFn != nil {
		_ = a.closeFn()
	}
}

// NewAdapter creates a new Redis adapter with the provided configuration.
func NewAdapter(config *Config) (*Adapter, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	a := &Adapter{}

	// Set default key if not provided
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
func NewAdapterBasic(network string, address string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address})
}

// NewAdapterWithUser creates adapter with user credentials.
// Deprecated: Use NewAdapter with Config struct instead.
func NewAdapterWithUser(network string, address string, username string, password string) (*Adapter, error) {
	return NewAdapter(&Config{
		Network:  network,
		Address:  address,
		Username: username,
		Password: password,
	})
}

// NewAdapterWithPassword creates adapter with password authentication.
// Deprecated: Use NewAdapter with Config struct instead.
func NewAdapterWithPassword(network string, address string, password string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address, Password: password})
}

// NewAdapterWithKey creates adapter with custom key.
// Deprecated: Use NewAdapter with Config struct instead.
func NewAdapterWithKey(network string, address string, key string) (*Adapter, error) {
	return NewAdapter(&Config{Network: network, Address: address, Key: key})
}

// NewAdapterWithClient creates adapter with an existing go-redis client.
// Deprecated: Use NewAdapter with Config struct instead.
// The adapter does not close client.
func NewAdapterWithClient(client redis.Cmdable) (*Adapter, error) {
	return NewAdapter(&Config{Client: client})
}

// NewAdapterWithClientAndOptions creates adapter with a client and options.
// Deprecated: Use NewAdapter with Config struct instead.
func NewAdapterWithClientAndOptions(client redis.Cmdable, options ...Option) (*Adapter, error) {
	a, err := NewAdapter(&Config{Client: client})
	if err != nil {
		return nil, err
	}
	for _, option := range options {
		option(a)
	}
	return a, nil
}

type Option func(*Adapter)

// NewAdapterWithOption creates adapter with options pattern.
// Deprecated: Use NewAdapter with Config struct instead.
func NewAdapterWithOption(options ...Option) (*Adapter, error) {
	a := &Adapter{}
	for _, option := range options {
		option(a)
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

func WithAddress(address string) Option {
	return func(a *Adapter) { a.address = address }
}

func WithUsername(username string) Option {
	return func(a *Adapter) { a.username = username }
}

func WithPassword(password string) Option {
	return func(a *Adapter) { a.password = password }
}

func WithNetwork(network string) Option {
	return func(a *Adapter) { a.network = network }
}

func WithKey(key string) Option {
	return func(a *Adapter) { a.key = key }
}

func WithTls(tlsConfig *tls.Config) Option {
	return func(a *Adapter) { a.tlsConfig = tlsConfig }
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

// LoadPolicy loads policy from database.
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

// SavePolicy saves policy to database.
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
		return a.client.Del(context.Background(), a.key).Err()
	}
	staging, err := snapshotKey(a.key)
	if err != nil {
		return err
	}
	return replacePolicyScript.Run(context.Background(), a.client, []string{a.key, staging}, args...).Err()
}

// AddPolicy adds a policy rule to the storage.
func (a *Adapter) AddPolicy(sec string, ptype string, rule []string) error {
	text, err := json.Marshal(savePolicyLine(ptype, rule))
	if err != nil {
		return err
	}
	return a.client.RPush(context.Background(), a.key, text).Err()
}

// RemovePolicy removes a policy rule from the storage.
func (a *Adapter) RemovePolicy(sec string, ptype string, rule []string) error {
	text, err := json.Marshal(savePolicyLine(ptype, rule))
	if err != nil {
		return err
	}
	return a.client.LRem(context.Background(), a.key, 1, text).Err()
}

// AddPolicies adds policy rules to the storage.
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

// RemovePolicies removes policy rules from the storage.
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

// IsFiltered returns true if the loaded policy has been filtered.
func (a *Adapter) IsFiltered() bool {
	return a.isFiltered.Load()
}

type Filter struct {
	PType []string
	V0    []string
	V1    []string
	V2    []string
	V3    []string
	V4    []string
	V5    []string
}

// matches compares decoded fields, never their JSON wire representation.
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

// LoadFilteredPolicy loads only policy rules that match the filter.
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

// RemoveFilteredPolicy removes policy rules that match the filter from storage.
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

// UpdatePolicy updates a policy rule in Redis.
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

// UpdatePolicies updates policy rules in Redis.
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

// UpdateFilteredPolicies replaces rules matched by the filter and returns the removed rules.
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
// KEYS[2]; RENAME is the final operation on the live key. Script errors therefore
// cannot leave a prefix of the new policy visible. The temporary namespace is
// private to this adapter. A lost reply can still mean a complete commit occurred.
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
