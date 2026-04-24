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

	lastDevErr       string
	lastDevCount     int
	lastLogicalCount int
	devErrMu         sync.RWMutex

	wakeCh         chan struct{}
	captureRequest uint64
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

func (a *AutoSwitchApp) SetDevState(err string, count int, logicalCount int) {
	a.devErrMu.Lock()
	defer a.devErrMu.Unlock()
	a.lastDevErr = err
	a.lastDevCount = count
	a.lastLogicalCount = logicalCount
}

func (a *AutoSwitchApp) LastDevError() string {
	a.devErrMu.RLock()
	defer a.devErrMu.RUnlock()
	return a.lastDevErr
}

func (a *AutoSwitchApp) DevCount() int {
	a.devErrMu.RLock()
	defer a.devErrMu.RUnlock()
	return a.lastDevCount
}

func (a *AutoSwitchApp) LogicalDevCount() int {
	a.devErrMu.RLock()
	defer a.devErrMu.RUnlock()
	return a.lastLogicalCount
}

func (a *AutoSwitchApp) Run(ctx context.Context) error {
	setLowPriorityDefaults(true, true)

	var last Applied
	var lastErr string

	for {
		a.reloadConfigIfChanged()
		cfg := a.CurrentConfig()

		switchMsg, errStr, devCount, logicalCount := tickOnce(cfg, &last)
		if switchMsg != "" {
			log.Print(switchMsg)
		}
		handleError(&lastErr, errStr)
		a.SetDevState(errStr, devCount, logicalCount)

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

func (a *AutoSwitchApp) ScheduleForegroundAppend(delay time.Duration) {
	a.mu.Lock()
	a.captureRequest++
	requestID := a.captureRequest
	a.mu.Unlock()

	log.Printf("[CFG] scheduled delayed foreground capture in %s", delay)

	go func() {
		timer := time.NewTimer(delay)
		defer timer.Stop()

		<-timer.C

		a.mu.RLock()
		stale := requestID != a.captureRequest
		a.mu.RUnlock()
		if stale {
			return
		}

		if err := a.appendForegroundProcess(requestID); err != nil {
			log.Printf("[CFG] delayed foreground append failed: %v", err)
		}
	}()
}

func (a *AutoSwitchApp) CancelForegroundAppend() {
	a.mu.Lock()
	a.captureRequest++
	a.mu.Unlock()
}

func (a *AutoSwitchApp) appendForegroundProcess(requestID uint64) error {
	proc, err := ForegroundProcessName()
	if err != nil {
		return fmt.Errorf("read foreground process: %w", err)
	}

	proc = normalizeProcessName(proc)
	if proc == "" {
		return fmt.Errorf("foreground process name is empty")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if requestID != a.captureRequest {
		return nil
	}

	next := cloneConfig(a.cfg)
	next.Whitelist = append(next.Whitelist, proc)
	next.WhitelistSet[proc] = struct{}{}

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

	log.Printf("[CFG] appended delayed foreground process: %s", proc)
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
