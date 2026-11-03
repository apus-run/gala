package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

// ErrManagerClosed is returned when using a closed Manager | 使用已关闭的 Manager 时返回 ErrManagerClosed。
var ErrManagerClosed = errors.New("eventbus: manager is closed")

// Manager manages multiple event buses and provides a global interface | Manager 管理多条事件总线并提供一个全局接口。
type Manager struct {
	mu        sync.RWMutex
	buses     map[string]PubSub
	global    PubSub
	logger    *slog.Logger
	busLogger *slog.Logger
	closed    bool
}

// NewManager creates a new event bus manager | NewManager 创建新的事件总线管理器。
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		buses:     make(map[string]PubSub),
		global:    NewEventBus(logger),
		logger:    logger.With("module", "eventbus/manager"),
		busLogger: logger,
	}
}

// GetBus returns an event bus by name, creates it if it doesn't exist | GetBus 按名字返回事件总线，不存在则创建。
func (m *Manager) GetBus(name string) (PubSub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bus, exists := m.buses[name]; exists {
		return bus, nil
	}
	if m.closed {
		return nil, ErrManagerClosed
	}

	// create a new bus
	// 创建新总线
	bus := NewEventBus(m.busLogger)
	m.buses[name] = bus
	m.logger.Info("Created new event bus", "name", name)

	return bus, nil
}

// Global returns the global event bus | Global 返回全局事件总线。
func (m *Manager) Global() (PubSub, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, ErrManagerClosed
	}
	return m.global, nil
}

// Publish publishes an event to a specific bus | Publish 向指定总线发布事件。
func (m *Manager) Publish(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.Publish(ctx, event)
}

// PublishAsync dispatches an event asynchronously on a named bus | PublishAsync 在命名总线上异步分发事件。
func (m *Manager) PublishAsync(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.PublishAsync(ctx, event)
}

// PublishAndWait dispatches an event concurrently and waits on a named bus | PublishAndWait 在命名总线上并发分发事件并等待完成。
func (m *Manager) PublishAndWait(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.PublishAndWait(ctx, event)
}

// PublishGlobal publishes an event to the global bus | PublishGlobal 向全局总线发布事件。
func (m *Manager) PublishGlobal(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.Publish(ctx, event)
}

// PublishGlobalAsync dispatches an event asynchronously on the global bus | PublishGlobalAsync 在全局总线上异步分发事件。
func (m *Manager) PublishGlobalAsync(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.PublishAsync(ctx, event)
}

// PublishGlobalAndWait dispatches concurrently and waits on the global bus | PublishGlobalAndWait 在全局总线上并发分发并等待完成。
func (m *Manager) PublishGlobalAndWait(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.PublishAndWait(ctx, event)
}

// Subscribe subscribes to events on a specific bus | Subscribe 订阅指定总线上的事件。
func (m *Manager) Subscribe(busName string, eventType EventType, handler Handler) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.Subscribe(eventType, handler)
}

// SubscribeGlobal subscribes to events on the global bus | SubscribeGlobal 订阅全局总线上的事件。
func (m *Manager) SubscribeGlobal(eventType EventType, handler Handler) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.Subscribe(eventType, handler)
}

// Close closes all event buses | Close 关闭所有事件总线。
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return fmt.Errorf("event bus manager already closed")
	}
	m.closed = true
	buses := m.buses
	m.buses = make(map[string]PubSub)
	global := m.global
	m.mu.Unlock()

	var errs []error
	for name, bus := range buses {
		if err := bus.Close(); err != nil {
			m.logger.Error("Error closing bus", "name", name, "error", err)
			errs = append(errs, fmt.Errorf("close bus %s: %w", name, err))
		}
	}

	if err := global.Close(); err != nil {
		m.logger.Error("Error closing global bus", "error", err)
		errs = append(errs, fmt.Errorf("close global bus: %w", err))
	}

	m.logger.Info("Event bus manager closed")

	return errors.Join(errs...)
}

// GetStats returns statistics for all buses | GetStats 返回所有总线的统计信息。
func (m *Manager) GetStats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]any)
	stats["total_buses"] = len(m.buses)

	busStats := make(map[string]any)
	for name, bus := range m.buses {
		if eventBus, ok := bus.(*EventBus); ok {
			busStats[name] = map[string]any{
				"event_types": eventBus.GetEventTypes(),
			}
		}
	}
	stats["buses"] = busStats

	if eventBus, ok := m.global.(*EventBus); ok {
		stats["global_bus"] = map[string]any{
			"event_types": eventBus.GetEventTypes(),
		}
	}

	return stats
}
