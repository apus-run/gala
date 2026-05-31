package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// Manager manages multiple event buses and provides a global interface
type Manager struct {
	mu        sync.RWMutex
	buses     map[string]PubSub
	global    PubSub
	logger    *slog.Logger
	busLogger *slog.Logger
	closed    bool
}

// NewManager creates a new event bus manager
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

// GetBus returns an event bus by name, creates it if it doesn't exist
func (m *Manager) GetBus(name string) PubSub {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bus, exists := m.buses[name]; exists {
		return bus
	}
	if m.closed {
		return m.global
	}

	// Create new bus
	bus := NewEventBus(m.busLogger)
	m.buses[name] = bus
	m.logger.Info("Created new event bus", "name", name)

	return bus
}

// Global returns the global event bus
func (m *Manager) Global() PubSub {
	return m.global
}

// Publish publishes an event to a specific bus
func (m *Manager) Publish(ctx context.Context, busName string, event *Event) error {
	bus := m.GetBus(busName)
	return bus.Publish(ctx, event)
}

// PublishGlobal publishes an event to the global bus
func (m *Manager) PublishGlobal(ctx context.Context, event *Event) error {
	return m.global.Publish(ctx, event)
}

// Subscribe subscribes to events on a specific bus
func (m *Manager) Subscribe(busName string, eventType EventType, handler Handler) error {
	bus := m.GetBus(busName)
	return bus.Subscribe(eventType, handler)
}

// SubscribeGlobal subscribes to events on the global bus
func (m *Manager) SubscribeGlobal(eventType EventType, handler Handler) error {
	return m.global.Subscribe(eventType, handler)
}

// Close closes all event buses
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("event bus manager already closed")
	}

	for name, bus := range m.buses {
		if err := bus.Close(); err != nil {
			m.logger.Error("Error closing bus", "name", name, "error", err)
		}
	}

	if err := m.global.Close(); err != nil {
		m.logger.Error("Error closing global bus", "error", err)
	}

	m.buses = make(map[string]PubSub)
	m.closed = true
	m.logger.Info("Event bus manager closed")

	return nil
}

// GetStats returns statistics for all buses
func (m *Manager) GetStats() map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["total_buses"] = len(m.buses)

	busStats := make(map[string]interface{})
	for name, bus := range m.buses {
		if eventBus, ok := bus.(*EventBus); ok {
			busStats[name] = map[string]interface{}{
				"event_types": eventBus.GetEventTypes(),
			}
		}
	}
	stats["buses"] = busStats

	if eventBus, ok := m.global.(*EventBus); ok {
		stats["global_bus"] = map[string]interface{}{
			"event_types": eventBus.GetEventTypes(),
		}
	}

	return stats
}
