package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
)

var ErrManagerClosed = errors.New("eventbus: manager is closed")

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
func (m *Manager) GetBus(name string) (PubSub, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if bus, exists := m.buses[name]; exists {
		return bus, nil
	}
	if m.closed {
		return nil, ErrManagerClosed
	}

	// Create new bus
	bus := NewEventBus(m.busLogger)
	m.buses[name] = bus
	m.logger.Info("Created new event bus", "name", name)

	return bus, nil
}

// Global returns the global event bus
func (m *Manager) Global() (PubSub, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, ErrManagerClosed
	}
	return m.global, nil
}

// Publish publishes an event to a specific bus
func (m *Manager) Publish(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.Publish(ctx, event)
}

// PublishAsync dispatches an event asynchronously on a named bus.
func (m *Manager) PublishAsync(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.PublishAsync(ctx, event)
}

// PublishAndWait dispatches an event concurrently and waits on a named bus.
func (m *Manager) PublishAndWait(ctx context.Context, busName string, event *Event) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.PublishAndWait(ctx, event)
}

// PublishGlobal publishes an event to the global bus
func (m *Manager) PublishGlobal(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.Publish(ctx, event)
}

// PublishGlobalAsync dispatches an event asynchronously on the global bus.
func (m *Manager) PublishGlobalAsync(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.PublishAsync(ctx, event)
}

// PublishGlobalAndWait dispatches concurrently and waits on the global bus.
func (m *Manager) PublishGlobalAndWait(ctx context.Context, event *Event) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.PublishAndWait(ctx, event)
}

// Subscribe subscribes to events on a specific bus
func (m *Manager) Subscribe(busName string, eventType EventType, handler Handler) error {
	bus, err := m.GetBus(busName)
	if err != nil {
		return err
	}
	return bus.Subscribe(eventType, handler)
}

// SubscribeGlobal subscribes to events on the global bus
func (m *Manager) SubscribeGlobal(eventType EventType, handler Handler) error {
	bus, err := m.Global()
	if err != nil {
		return err
	}
	return bus.Subscribe(eventType, handler)
}

// Close closes all event buses
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

// GetStats returns statistics for all buses
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
