# EventBus Package

A flexible and powerful event bus implementation for Go that enables loose coupling between components through event-driven communication.

Original author: [tx7do](https://github.com/tx7do)

## Features

- **Flexible Event Data**: Events can carry any type of data
- **Type-Safe Handlers**: Subscribe handlers to specific event types
- **Async Support**: Both synchronous and asynchronous event publishing
- **Middleware Support**: Chain middlewares for logging, recovery, timeout, retry, etc.
- **Multiple Event Buses**: Manage multiple isolated event buses
- **Once Handlers**: Subscribe handlers that execute only once
- **Thread-Safe**: Safe for concurrent use

## Installation

```bash
go get github.com/apus-run/gala/components/eventbus
```

## Quick Start

### Creating an Event Bus

```go
import (
    "context"
    "log/slog"
    "github.com/apus-run/gala/components/eventbus"
)

// Create a new event bus
bus := eventbus.NewEventBus(slog.Default())
defer bus.Close()
```

### Publishing Events

```go
// Create an event with any data
event := eventbus.NewEvent("email.received", map[string]interface{}{
    "from": "user@example.com",
    "subject": "Hello World",
})

// Publish the event
ctx := context.Background()
if err := bus.Publish(ctx, event); err != nil {
    slog.Error("Failed to publish event", "error", err)
}

// Or publish asynchronously
bus.PublishAsync(ctx, event)
```

### Subscribing to Events

```go
// Subscribe with a handler function
bus.Subscribe("email.received", eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    slog.Info("Received email event", "data", event.Data)

    // Extract typed data
    var emailData struct {
        From    string `json:"from"`
        Subject string `json:"subject"`
    }
    if err := event.GetData(&emailData); err != nil {
        return err
    }

    slog.Info("Email received", "from", emailData.From, "subject", emailData.Subject)
    return nil
}))
```

### Using Event Manager

```go
// Create an event bus manager
manager := eventbus.NewManager(slog.Default())
defer manager.Close()

// Subscribe to global bus
manager.SubscribeGlobal("user.created", eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    slog.Info("User created", "data", event.Data)
    return nil
}))

// Publish to global bus
event := eventbus.NewEvent("user.created", eventbus.UserCreatedEvent{
    UserID:   123,
    Username: "john_doe",
    Email:    "john@example.com",
})
manager.PublishGlobal(ctx, event)

// Use named buses for isolation
emailBus := manager.GetBus("email")
emailBus.Subscribe("email.received", handler)
```

## Advanced Usage

### Using Middlewares

```go
import "github.com/apus-run/gala/components/eventbus"

logger := slog.Default()

// Chain multiple middlewares
middleware := eventbus.Chain(
    eventbus.LoggingMiddleware(logger),
    eventbus.RecoveryMiddleware(logger),
    eventbus.TimeoutMiddleware(5*time.Second),
    eventbus.RetryMiddleware(3, time.Second),
    eventbus.MetricsMiddleware(logger),
)

// Wrap handler with middleware
handler := eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    // Your handler logic
    return nil
})

wrappedHandler := middleware(handler)
bus.Subscribe("email.received", wrappedHandler)
```

### Filter Handlers

```go
// Only handle high-priority events
filterHandler := eventbus.NewFilterHandler(
    func(event *eventbus.Event) bool {
        return event.Priority >= 5
    },
    eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
        slog.Info("Handling high-priority event", "type", event.Type)
        return nil
    }),
)

bus.Subscribe("email.received", filterHandler)
```

### Chain Handlers

```go
// Execute multiple handlers in sequence
handler1 := eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    log.Info("Handler 1")
    return nil
})

handler2 := eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    log.Info("Handler 2")
    return nil
})

chainHandler := eventbus.NewChainHandler(handler1, handler2)
bus.Subscribe("task.completed", chainHandler)
```

### Subscribe Once

```go
// Handler will be called only once
bus.SubscribeOnce("system.started", eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    log.Info("System started - this will only run once")
    return nil
}))
```

## Predefined Event Types

The package includes common event types:

```go
// Email events
eventbus.EventEmailReceived
eventbus.EventEmailSent
eventbus.EventEmailDeleted
eventbus.EventEmailRead
eventbus.EventEmailFlagged
eventbus.EventEmailProcessed
eventbus.EventEmailFailed

// User events
eventbus.EventUserCreated
eventbus.EventUserUpdated
eventbus.EventUserDeleted
eventbus.EventUserLoggedIn
eventbus.EventUserLoggedOut

// Task events
eventbus.EventTaskCreated
eventbus.EventTaskStarted
eventbus.EventTaskCompleted
eventbus.EventTaskFailed
eventbus.EventTaskCancelled

// System events
eventbus.EventSystemStarted
eventbus.EventSystemStopped
eventbus.EventSystemError
```

## Event Metadata

Add metadata to events for additional context:

```go
event := eventbus.NewEvent("email.received", emailData).
    WithSource("imap-processor").
    WithPriority(5).
    WithMetadata("tenant_id", "123").
    WithMetadata("user_id", "456")

bus.Publish(ctx, event)
```

## Best Practices

1. **Use Typed Events**: Define structs for event data for better type safety
2. **Error Handling**: Always handle errors in event handlers
3. **Async for Non-Critical**: Use async publishing for non-critical events
4. **Middleware Order**: Place recovery middleware first in the chain
5. **Close Resources**: Always close event buses when done
6. **Event Naming**: Use dot notation for hierarchical event types (e.g., "email.received")

## Example: Email Processing

```go
// Define event data
type EmailReceivedData struct {
    EmailID   string
    From      string
    To        string
    Subject   string
    Body      string
}

// Create event bus
bus := eventbus.NewEventBus(logger)

// Subscribe to email events
bus.Subscribe(eventbus.EventEmailReceived, eventbus.EventHandlerFunc(func(ctx context.Context, event *eventbus.Event) error {
    var data EmailReceivedData
    if err := event.GetData(&data); err != nil {
        return err
    }

    // Process email
    slog.Info("Processing email", "from", data.From, "subject", data.Subject)

    // Trigger another event
    processedEvent := eventbus.NewEvent(eventbus.EventEmailProcessed, map[string]interface{}{
        "email_id": data.EmailID,
        "success": true,
    })
    return bus.Publish(ctx, processedEvent)
}))

// Publish email received event
emailEvent := eventbus.NewEvent(eventbus.EventEmailReceived, EmailReceivedData{
    EmailID: "123",
    From:    "sender@example.com",
    To:      "receiver@example.com",
    Subject: "Test Email",
    Body:    "Hello World",
})

bus.Publish(ctx, emailEvent)
```

## Thread Safety

All event bus operations are thread-safe and can be used concurrently from multiple goroutines.
