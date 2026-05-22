package limiter

import (
	"cct/config"
	"context"
	"log"
	"time"
)

type Manager struct {
	chans  map[string]chan struct{}
	stopCh chan struct{}
}

func NewManager(providers map[string]config.Provider) *Manager {
	m := &Manager{
		chans:  make(map[string]chan struct{}),
		stopCh: make(chan struct{}),
	}

	for name, provider := range providers {
		if provider.Limits.RPM > 0 {
			log.Printf("Rate limiting enabled for provider %s: %d RPM", name, provider.Limits.RPM)
			ch := make(chan struct{}, provider.Limits.RPM)
			// Fill the bucket initially
			for i := 0; i < provider.Limits.RPM; i++ {
				ch <- struct{}{}
			}
			m.chans[name] = ch

			// Refill periodically
			go func(pName string, rpm int, c chan struct{}) {
				interval := time.Duration(float64(time.Minute) / float64(rpm))
				ticker := time.NewTicker(interval)
				defer ticker.Stop()
				for {
					select {
					case <-m.stopCh:
						return
					case <-ticker.C:
						select {
						case c <- struct{}{}:
						default:
						}
					}
				}
			}(name, provider.Limits.RPM, ch)
		}
	}

	return m
}

// Wait blocks until a token is available for the given provider or the context is done.
// If the context is cancelled (e.g. client disconnected), the wait is silently abandoned.
func (m *Manager) Wait(ctx context.Context, providerName string) {
	ch, exists := m.chans[providerName]
	if !exists {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
	}
}

// Stop terminates all refill goroutines and releases resources.
func (m *Manager) Stop() {
	close(m.stopCh)
}
