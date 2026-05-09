package limiter

import (
	"cct/config"
	"log"
	"time"
)

type Manager struct {
	chans map[string]chan struct{}
}

func NewManager(providers map[string]config.Provider) *Manager {
	m := &Manager{
		chans: make(map[string]chan struct{}),
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
				interval := 60 * 1000 / rpm
				ticker := time.NewTicker(time.Duration(interval) * time.Millisecond)
				for range ticker.C {
					select {
					case c <- struct{}{}:
					default:
					}
				}
			}(name, provider.Limits.RPM, ch)
		}
	}

	return m
}

func (m *Manager) Wait(providerName string) {
	if ch, exists := m.chans[providerName]; exists {
		<-ch
	}
}
