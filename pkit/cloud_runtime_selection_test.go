package pkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestCloudDefaultsReachRuntimeConstructors(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name := "available"
		if unavailable {
			name = "unavailable"
		}
		t.Run(name, func(t *testing.T) {
			cfg := onOneDatabase(t)
			cfg.Cloud.Profile = config.ProfileAWS
			cfg.Files.Store = config.StoreLocalDisk
			cfg.Cache = config.Cache{URL: "valkey://cache.example.com:6379", App: "collect"}
			cfg.NATS.URL = "nats://broker.example.com:4222"
			cacheCalls, brokerCalls, memoryCalls := 0, 0, 0
			var readCache config.Cache
			var readBroker config.NATS
			unreachable := errors.New("shared cache unavailable")
			d := pkit.Deployment{
				Environment: pkit.Development, Config: cfg,
				Caches: app.Caches{Valkey: func(_ context.Context, c config.Cache) (cache.Cache, error) {
					cacheCalls++
					readCache = c
					if unavailable {
						return nil, unreachable
					}
					return cache.Memory(c.App), nil
				}},
				Transports: app.Transports{
					Memory: func() events.Transport {
						memoryCalls++
						return memory.New()
					},
					JetStream: func(c config.NATS) (events.Transport, error) {
						brokerCalls++
						readBroker = c
						return memory.New(), nil
					},
				},
			}
			a := pkit.NewApp("collect").Use(doors, desk)
			if _, err := a.Describe(d); err != nil {
				t.Fatalf("describe: %v", err)
			}
			if cacheCalls+brokerCalls+memoryCalls != 0 {
				t.Fatal("Describe opened a provider")
			}
			runtime, err := a.Build(t.Context(), d)
			if runtime != nil {
				t.Cleanup(func() { _ = runtime.Close() })
			}
			if unavailable {
				if runtime != nil || !errors.Is(err, unreachable) {
					t.Errorf("unavailable selected provider: runtime=%v error=%v", runtime, err)
				}
			} else {
				if err != nil || runtime == nil {
					t.Fatalf("Build: runtime=%v error=%v", runtime, err)
				}
				if brokerCalls != 1 || readBroker.Transport != "jetstream" || readBroker.URL != cfg.NATS.URL {
					t.Errorf("broker constructor: calls=%d settings=%+v", brokerCalls, readBroker)
				}
			}
			if cacheCalls != 1 || readCache.Adapter != "valkey" || readCache.URL != cfg.Cache.URL {
				t.Errorf("cache constructor: calls=%d settings=%+v", cacheCalls, readCache)
			}
			if memoryCalls != 0 {
				t.Errorf("cloud transport fell back to memory %d times", memoryCalls)
			}
			if cfg.Cache.Adapter != "" || cfg.NATS.Transport != "" {
				t.Error("Build mutated its caller's configuration")
			}
		})
	}
}
