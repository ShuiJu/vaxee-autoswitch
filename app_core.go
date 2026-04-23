package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

type ProfileKind int

const (
	ProfileHit ProfileKind = iota
	ProfileDefault
)

type AutoSwitchApp struct {
	cfgPath string

	mu      sync.RWMutex
	cfg     *Config
	modTime time.Time

	wakeCh chan struct{}
}

func NewAutoSwitchApp(cfgPath string) (*AutoSwitchApp, error) {
	if err := ensureConfigExists(cfgPath); err != nil {
		return nil, fmt.Errorf("无法创建配置文件: %w", err)
	}

	cfg, modTime, err := loadConfig(cfgPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}

	return &AutoSwitchApp{
		cfgPath: cfgPath,
		cfg:     cfg,
		modTime: modTime,
		wakeCh:  make(chan struct{}, 1),
	}, nil
}

func (a *AutoSwitchApp) CurrentConfig() *Config {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return cloneConfig(a.cfg)
}

func (a *AutoSwitchApp) Run(ctx context.Context) error {
	setLowPriorityDefaults(true, true)

	var last Applied
	var lastErr string

	for {
		a.reloadConfigIfChanged()
		cfg := a.CurrentConfig()

		switchMsg, errStr := tickOnce(cfg, &last)
		if switchMsg != "" {
			log.Print(switchMsg)
		}
		handleError(&lastErr, errStr)

		timer := time.NewTimer(cfg.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-a.wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
		}
	}
}

func (a *AutoSwitchApp) UpdateProfile(profile ProfileKind, perf PerfMode, poll PollingRate, traj TrajectoryMode) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	next := cloneConfig(a.cfg)
	switch profile {
	case ProfileHit:
		next.HitMode = perf
		next.HitPoll = poll
		next.HitTraj = traj
	case ProfileDefault:
		next.DefaultMode = perf
		next.DefaultPoll = poll
		next.DefaultTraj = traj
	default:
		return fmt.Errorf("unknown profile")
	}

	if err := saveConfig(a.cfgPath, next); err != nil {
		return err
	}

	reloaded, modTime, err := loadConfig(a.cfgPath)
	if err != nil {
		return err
	}
	a.cfg = reloaded
	a.modTime = modTime
	a.signalWake()
	return nil
}

func (a *AutoSwitchApp) reloadConfigIfChanged() {
	a.mu.Lock()
	defer a.mu.Unlock()

	fi, err := os.Stat(a.cfgPath)
	if err != nil || !fi.ModTime().After(a.modTime) {
		return
	}

	cfg, modTime, err := loadConfig(a.cfgPath)
	if err != nil {
		return
	}

	a.cfg = cfg
	a.modTime = modTime
}

func (a *AutoSwitchApp) signalWake() {
	select {
	case a.wakeCh <- struct{}{}:
	default:
	}
}
