package consul

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apus-run/gala/registry"
)

// Reserved Meta keys. User metadata must not start with metaPrefix.
// 保留的 Meta key。用户元数据不得使用 metaPrefix 开头。
const (
	metaPrefix    = "gala."
	metaID        = "gala.id"
	metaVersion   = "gala.version"
	metaEndpoints = "gala.endpoints"

	// Consul limits a Meta value to 512 bytes.
	// Consul 限制单个 Meta 值最大 512 字节。
	maxMetaValue = 512
)

// ErrReservedMetadata is returned when instance metadata uses the "gala."
// key prefix, which this plugin keeps for itself.
//
// ErrReservedMetadata 在实例元数据使用了本插件保留的 "gala." key 前缀时返回.
var ErrReservedMetadata = errors.New("consul registry: metadata key uses reserved prefix \"gala.\"")

// agentService is the body of PUT /v1/agent/service/register.
// agentService 是 PUT /v1/agent/service/register 的请求体.
type agentService struct {
	ID      string            `json:"ID"`
	Name    string            `json:"Name"`
	Address string            `json:"Address,omitempty"`
	Port    int               `json:"Port,omitempty"`
	Meta    map[string]string `json:"Meta,omitempty"`
	Check   *agentCheck       `json:"Check,omitempty"`
}

type agentCheck struct {
	CheckID                        string `json:"CheckID"`
	Name                           string `json:"Name"`
	TTL                            string `json:"TTL"`
	Status                         string `json:"Status"`
	DeregisterCriticalServiceAfter string `json:"DeregisterCriticalServiceAfter,omitempty"`
}

// healthEntry is one element of the answer of /v1/health/service/<name>.
// healthEntry 是 /v1/health/service/<name> 应答中的一个元素.
type healthEntry struct {
	Node struct {
		Address string `json:"Address"`
	} `json:"Node"`
	Service struct {
		ID      string            `json:"ID"`
		Service string            `json:"Service"`
		Address string            `json:"Address"`
		Port    int               `json:"Port"`
		Meta    map[string]string `json:"Meta"`
	} `json:"Service"`
}

// serviceID is the Consul service ID. Consul scopes it to the agent, so it
// carries the name: two services may well use the same instance ID.
// serviceID 是 Consul 的 service ID。它由 agent 限定作用范围，因此要带上
// 服务名：两个服务完全可能使用同一个实例 ID。
func serviceID(name, id string) string { return name + "/" + id }

// checkID is the ID Consul gives the inline check of a service.
// checkID 是 Consul 为服务内联检查分配的 ID.
func checkID(svcID string) string { return "service:" + svcID }

func toAgentService(ins *registry.ServiceInstance, o *Options) (*agentService, error) {
	meta := make(map[string]string, len(ins.Metadata)+3)
	for k, v := range ins.Metadata {
		if strings.HasPrefix(k, metaPrefix) {
			return nil, fmt.Errorf("%w: %q", ErrReservedMetadata, k)
		}
		meta[k] = v
	}
	endpoints, err := json.Marshal(ins.Endpoints)
	if err != nil {
		return nil, fmt.Errorf("consul registry: encode endpoints: %w", err)
	}
	if len(endpoints) > maxMetaValue {
		return nil, fmt.Errorf("consul registry: endpoints take %d bytes, Consul allows %d per meta value",
			len(endpoints), maxMetaValue)
	}
	meta[metaID] = ins.ID
	meta[metaVersion] = ins.Version
	meta[metaEndpoints] = string(endpoints)

	svcID := serviceID(ins.Name, ins.ID)
	addr, port := hostPort(ins.Endpoints)
	check := &agentCheck{
		CheckID: checkID(svcID),
		Name:    "gala ttl",
		TTL:     secs(o.ttl),
		// healthy right away; the keepalive keeps it so
		// 注册即健康；续约保持这一状态
		Status: "passing",
	}
	if o.deregisterAfter > 0 {
		check.DeregisterCriticalServiceAfter = o.deregisterAfter.String()
	}
	return &agentService{
		ID: svcID, Name: ins.Name, Address: addr, Port: port, Meta: meta, Check: check,
	}, nil
}

// secs renders a duration as Consul's whole-second TTL, rounding up.
// secs 把时长渲染为 Consul 的整秒 TTL，向上取整.
func secs(d time.Duration) string {
	return strconv.FormatInt(int64((d+time.Second-1)/time.Second), 10) + "s"
}

// hostPort picks Address and Port from the first endpoint that has a host.
// hostPort 从第一个带主机的 endpoint 中选出 Address 与 Port.
func hostPort(endpoints []string) (string, int) {
	for _, e := range endpoints {
		u, err := url.Parse(e)
		if err != nil || u.Hostname() == "" {
			continue
		}
		port, _ := strconv.Atoi(u.Port())
		return u.Hostname(), port
	}
	return "", 0
}

func fromHealthEntry(e healthEntry) *registry.ServiceInstance {
	s := e.Service
	ins := &registry.ServiceInstance{Name: s.Service}

	if id, ok := s.Meta[metaID]; ok {
		ins.ID = id
		ins.Version = s.Meta[metaVersion]
		if raw := s.Meta[metaEndpoints]; raw != "" {
			// a bad value falls back below
			// 值坏了也没关系，下面有兜底
			_ = json.Unmarshal([]byte(raw), &ins.Endpoints)
		}
	} else {
		// registered by another tool
		// 由其它工具注册的
		ins.ID = s.ID
	}

	for k, v := range s.Meta {
		if strings.HasPrefix(k, metaPrefix) {
			continue
		}
		if ins.Metadata == nil {
			ins.Metadata = make(map[string]string, len(s.Meta))
		}
		ins.Metadata[k] = v
	}

	if len(ins.Endpoints) == 0 {
		host := s.Address
		if host == "" {
			host = e.Node.Address
		}
		if host != "" {
			ins.Endpoints = []string{"tcp://" + net.JoinHostPort(host, strconv.Itoa(s.Port))}
		}
	}
	return ins
}
