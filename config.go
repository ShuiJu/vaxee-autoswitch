package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const configFileName = "vaxee_autoswitch.conf"

// ---------------- Perf/Poll ----------------

type PerfMode byte

const (
	// 按你修正后的映射（基于抓包 Data Fragment）：
	// competitive_ms_off = 0x01
	// standard_ms_off    = 0x02
	// competitive_ms_on  = 0x03
	// standard_ms_on     = 0x04
	PerfCompetitiveMSOff PerfMode = 0x01
	PerfStandardMSOff    PerfMode = 0x02
	PerfCompetitiveMSOn  PerfMode = 0x03
	PerfStandardMSOn     PerfMode = 0x04
)

type PollingRate int

const (
	Poll1000 PollingRate = 1000
	Poll2000 PollingRate = 2000
	Poll4000 PollingRate = 4000
)

// ---------------- Trajectory ----------------
// 追踪轨迹：顺滑灵敏/稳定易控
// 抓包确认：cmd=0x13，byte3=0x02 表示“顺滑灵敏”，byte3=0x01 表示“稳定易控”。[1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)
type TrajectoryMode byte

const (
	TrajSmoothSensitive TrajectoryMode = 2 // 顺滑灵敏：0e a5 13 02 01 00 ... [1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)
	TrajStableControl   TrajectoryMode = 1 // 稳定易控：0e a5 13 01 01 00 ... [1](https://maynoothuniversity-my.sharepoint.com/personal/shengwei_huang_2022_mumail_ie/Documents/Microsoft%20Copilot%20Chat%20Files/config.txt)
)

func trajName(t TrajectoryMode) string {
	switch t {
	case TrajSmoothSensitive:
		return "smooth_sensitive"
	case TrajStableControl:
		return "stable_control"
	default:
		return fmt.Sprintf("0x%02x", byte(t))
	}
}

func parseTraj(s string) (TrajectoryMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "smooth_sensitive", "smooth", "sensitive", "smoothsens":
		return TrajSmoothSensitive, nil
	case "stable_control", "stable", "control", "stablectrl":
		return TrajStableControl, nil
	default:
		return 0, fmt.Errorf("unknown trajectory mode: %s", s)
	}
}

// ---------------- Config ----------------

type Config struct {
	Interval time.Duration

	HitMode PerfMode
	HitPoll PollingRate
	HitTraj TrajectoryMode

	DefaultMode PerfMode
	DefaultPoll PollingRate
	DefaultTraj TrajectoryMode

	Whitelist    []string
	WhitelistSet map[string]struct{}
	ConfigPath   string
}

func defaultConfigText() string {
	// 预设建议：
	// 命中白名单：competitive_ms_off + 1000Hz + 顺滑灵敏（更跟手）
	// 未命中：standard_ms_off + 1000Hz + 稳定易控（更稳）
	return `# VAXEE AutoSwitch 配置文件
# --------------------------------------------
# 说明：
# 1) 以 key=value 配置策略
# 2) 其余非空、非 # 开头的行，会被当作“白名单程序名”（每行一个，例如 cs2.exe）
#
# 可配置项：
# interval_seconds=60
#
# hit_mode=competitive_ms_off      # standard_ms_off / competitive_ms_off / competitive_ms_on / standard_ms_on
# hit_poll=1000                    # 1000 / 2000 / 4000
# hit_traj=smooth_sensitive        # smooth_sensitive / stable_control (顺滑灵敏/稳定易控)
#
# default_mode=standard_ms_off
# default_poll=1000
# default_traj=smooth_sensitive
#
# --------------------------------------------
interval_seconds=60

hit_mode=competitive_ms_off
hit_poll=1000
hit_traj=smooth_sensitive

default_mode=standard_ms_off
default_poll=1000
default_traj=smooth_sensitive

# 白名单示例（每行一个进程名）：
# cs2.exe
# valorant.exe
`
}

func ensureConfigExists(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, []byte(defaultConfigText()), 0644)
}

func loadConfig(path string) (*Config, time.Time, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}, err
	}

	cfg := &Config{
		Interval: 60 * time.Second,

		HitMode: PerfCompetitiveMSOff,
		HitPoll: Poll1000,
		HitTraj: TrajSmoothSensitive,

		DefaultMode: PerfStandardMSOff,
		DefaultPoll: Poll1000,
		DefaultTraj: TrajStableControl,

		Whitelist:    []string{},
		WhitelistSet: map[string]struct{}{},
		ConfigPath:   path,
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if i := strings.IndexByte(line, '='); i > 0 {
			key := strings.ToLower(strings.TrimSpace(line[:i]))
			val := strings.TrimSpace(line[i+1:])

			switch key {
			case "interval_seconds":
				sec, e := parseInt(val)
				if e != nil || sec <= 0 {
					return nil, time.Time{}, fmt.Errorf("invalid interval_seconds: %s", val)
				}
				cfg.Interval = time.Duration(sec) * time.Second

			case "hit_mode":
				m, e := parsePerf(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.HitMode = m

			case "hit_poll":
				n, e := parseInt(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.HitPoll = PollingRate(n)
				if _, e := pollingToYY(cfg.HitPoll); e != nil {
					return nil, time.Time{}, e
				}

			case "hit_traj":
				t, e := parseTraj(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.HitTraj = t

			case "default_mode":
				m, e := parsePerf(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.DefaultMode = m

			case "default_poll":
				n, e := parseInt(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.DefaultPoll = PollingRate(n)
				if _, e := pollingToYY(cfg.DefaultPoll); e != nil {
					return nil, time.Time{}, e
				}

			case "default_traj":
				t, e := parseTraj(val)
				if e != nil {
					return nil, time.Time{}, e
				}
				cfg.DefaultTraj = t

			default:
				// 未知 key 忽略，便于扩展
			}
			continue
		}

		// 白名单行：只取 basename，转小写
		proc := strings.ToLower(filepath.Base(line))
		cfg.Whitelist = append(cfg.Whitelist, proc)
		cfg.WhitelistSet[proc] = struct{}{}
	}

	if err := sc.Err(); err != nil {
		return nil, time.Time{}, err
	}
	return cfg, fi.ModTime(), nil
}

func parseInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty int")
	}
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("not int: %s", s)
		}
		n = n*10 + int(ch-'0')
	}
	return n, nil
}

func parsePerf(s string) (PerfMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "standard_ms_off":
		return PerfStandardMSOff, nil
	case "competitive_ms_off":
		return PerfCompetitiveMSOff, nil
	case "competitive_ms_on":
		return PerfCompetitiveMSOn, nil
	case "standard_ms_on":
		return PerfStandardMSOn, nil
	default:
		return 0, fmt.Errorf("unknown perf mode: %s", s)
	}
}

func perfName(p PerfMode) string {
	switch p {
	case PerfStandardMSOff:
		return "standard_ms_off"
	case PerfCompetitiveMSOff:
		return "competitive_ms_off"
	case PerfCompetitiveMSOn:
		return "competitive_ms_on"
	case PerfStandardMSOn:
		return "standard_ms_on"
	default:
		return fmt.Sprintf("0x%02x", byte(p))
	}
}

// 回报率映射：按抓包分段标注（1000/2000/4000）
// 1000->0x02, 2000->0x03, 4000->0x04
func pollingToYY(p PollingRate) (byte, error) {
	switch p {
	case Poll1000:
		return 0x02, nil
	case Poll2000:
		return 0x03, nil
	case Poll4000:
		return 0x04, nil
	default:
		return 0, fmt.Errorf("unsupported polling rate: %d", p)
	}
}
