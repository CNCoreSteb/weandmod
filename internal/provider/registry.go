package provider

import "sync"

var (
	regMu    sync.RWMutex
	registry = map[string]Provider{}
	regOrder []string
)

// Register 把提供方加入全局注册表，供 init() 调用。
// ID 为空或重复时直接 panic（启动即暴露配置错误）。
func Register(p Provider) {
	regMu.Lock()
	defer regMu.Unlock()
	id := p.ID()
	if id == "" {
		panic("provider: empty ID")
	}
	if _, dup := registry[id]; dup {
		panic("provider: duplicate ID " + id)
	}
	registry[id] = p
	regOrder = append(regOrder, id)
}

// Get 按 ID 查找提供方。
func Get(id string) (Provider, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// All 按注册顺序返回全部提供方。
func All() []Provider {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Provider, 0, len(regOrder))
	for _, id := range regOrder {
		out = append(out, registry[id])
	}
	return out
}
